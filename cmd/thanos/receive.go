package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/run"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/tsdb"
	"github.com/prometheus/tsdb/labels"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/component"
	"github.com/thanos-io/thanos/pkg/objstore/client"
	"github.com/thanos-io/thanos/pkg/receive"
	"github.com/thanos-io/thanos/pkg/runutil"
	"github.com/thanos-io/thanos/pkg/shipper"
	"github.com/thanos-io/thanos/pkg/store"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"google.golang.org/grpc"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

func registerReceive(m map[string]setupFunc, app *kingpin.Application, name string) {
	cmd := app.Command(name, "Accept Prometheus remote write API requests and write to local tsdb (EXPERIMENTAL, this may change drastically without notice)")

	grpcBindAddr, cert, key, clientCA := regGRPCFlags(cmd)
	httpMetricsBindAddr := regHTTPAddrFlag(cmd)

	remoteWriteAddress := cmd.Flag("remote-write.address", "Address to listen on for remote write requests.").
		Default("0.0.0.0:19291").String()

	dataDir := cmd.Flag("tsdb.path", "Data directory of TSDB.").
		Default("./data").String()

	labelStrs := cmd.Flag("labels", "External labels to announce. This flag will be removed in the future when handling multiple tsdb instances is added.").PlaceHolder("key=\"value\"").Strings()

	objStoreConfig := regCommonObjStoreFlags(cmd, "", false)

	retention := modelDuration(cmd.Flag("tsdb.retention", "How long to retain raw samples on local storage. 0d - disables this retention").Default("15d"))

	hashringsFile := cmd.Flag("receive.hashrings-file", "Path to file that contains the hashring configuration.").
		PlaceHolder("<path>").String()

	refreshInterval := modelDuration(cmd.Flag("receive.hashrings-file-refresh-interval", "Refresh interval to re-read the hashring configuration file. (used as a fallback)").
		Default("5m"))

	local := cmd.Flag("receive.local-endpoint", "Endpoint of local receive node. Used to identify the local node in the hashring configuration.").String()

	tenantHeader := cmd.Flag("receive.tenant-header", "HTTP header to determine tenant for write requests.").Default("THANOS-TENANT").String()

	replicaHeader := cmd.Flag("receive.replica-header", "HTTP header specifying the replica number of a write request.").Default("THANOS-REPLICA").String()

	replicationFactor := cmd.Flag("receive.replication-factor", "How many times to replicate incoming write requests.").Default("1").Uint64()

	m[name] = func(g *run.Group, logger log.Logger, reg *prometheus.Registry, tracer opentracing.Tracer, _ bool) error {
		lset, err := parseFlagLabels(*labelStrs)
		if err != nil {
			return errors.Wrap(err, "parse labels")
		}

		var cw *receive.ConfigWatcher
		if *hashringsFile != "" {
			cw, err = receive.NewConfigWatcher(log.With(logger, "component", "config-watcher"), reg, *hashringsFile, *refreshInterval)
			if err != nil {
				return err
			}
		}

		// Local is empty, so try to generate a local endpoint
		// based on the hostname and the listening port.
		if *local == "" {
			hostname, err := os.Hostname()
			if hostname == "" || err != nil {
				return errors.New("--receive.local-endpoint is empty and host could not be determined.")
			}
			parts := strings.Split(*remoteWriteAddress, ":")
			port := parts[len(parts)-1]
			*local = fmt.Sprintf("http://%s:%s/api/v1/receive", hostname, port)
		}

		return runReceive(
			g,
			logger,
			reg,
			tracer,
			*grpcBindAddr,
			*cert,
			*key,
			*clientCA,
			*httpMetricsBindAddr,
			*remoteWriteAddress,
			*dataDir,
			objStoreConfig,
			lset,
			*retention,
			cw,
			*local,
			*tenantHeader,
			*replicaHeader,
			*replicationFactor,
		)
	}
}

func runReceive(
	g *run.Group,
	logger log.Logger,
	reg *prometheus.Registry,
	tracer opentracing.Tracer,
	grpcBindAddr string,
	cert string,
	key string,
	clientCA string,
	httpMetricsBindAddr string,
	remoteWriteAddress string,
	dataDir string,
	objStoreConfig *pathOrContent,
	lset labels.Labels,
	retention model.Duration,
	cw *receive.ConfigWatcher,
	endpoint string,
	tenantHeader string,
	replicaHeader string,
	replicationFactor uint64,
) error {
	logger = log.With(logger, "component", "receive")
	level.Warn(logger).Log("msg", "setting up receive; the Thanos receive component is EXPERIMENTAL, it may break significantly without notice")

	tsdbCfg := &tsdb.Options{
		RetentionDuration: retention,
		NoLockfile:        true,
		MinBlockDuration:  model.Duration(time.Hour * 2),
		MaxBlockDuration:  model.Duration(time.Hour * 2),
	}

	localStorage := &tsdb.ReadyStorage{}
	receiver := receive.NewWriter(log.With(logger, "component", "receive-writer"), localStorage)
	webHandler := receive.NewHandler(log.With(logger, "component", "receive-handler"), &receive.Options{
		Receiver:          receiver,
		ListenAddress:     remoteWriteAddress,
		Registry:          reg,
		ReadyStorage:      localStorage,
		Endpoint:          endpoint,
		TenantHeader:      tenantHeader,
		ReplicaHeader:     replicaHeader,
		ReplicationFactor: replicationFactor,
	})

	// Start all components while we wait for TSDB to open but only load
	// initial config and mark ourselves as ready after it completed.
	dbOpen := make(chan struct{})
	level.Debug(logger).Log("msg", "setting up tsdb")
	{
		// TSDB.
		cancel := make(chan struct{})
		g.Add(
			func() error {
				level.Info(logger).Log("msg", "starting TSDB ...")
				db, err := tsdb.Open(
					dataDir,
					log.With(logger, "component", "tsdb"),
					reg,
					tsdbCfg,
				)
				if err != nil {
					close(dbOpen)
					return fmt.Errorf("opening storage failed: %s", err)
				}
				level.Info(logger).Log("msg", "tsdb started")

				startTimeMargin := int64(2 * time.Duration(tsdbCfg.MinBlockDuration).Seconds() * 1000)
				localStorage.Set(db, startTimeMargin)
				webHandler.StorageReady()
				level.Info(logger).Log("msg", "server is ready to receive web requests.")
				close(dbOpen)
				<-cancel
				return nil
			},
			func(err error) {
				if err := localStorage.Close(); err != nil {
					level.Error(logger).Log("msg", "error stopping storage", "err", err)
				}
				close(cancel)
			},
		)
	}

	level.Debug(logger).Log("msg", "setting up hashring")
	{
		updates := make(chan receive.Hashring)
		if cw != nil {
			ctx, cancel := context.WithCancel(context.Background())
			g.Add(func() error {
				receive.HashringFromConfig(ctx, updates, cw)
				return nil
			}, func(error) {
				cancel()
				close(updates)
			})
		} else {
			cancel := make(chan struct{})
			g.Add(func() error {
				updates <- receive.SingleNodeHashring(endpoint)
				<-cancel
				return nil
			}, func(error) {
				close(cancel)
				close(updates)
			})
		}

		cancel := make(chan struct{})
		g.Add(
			func() error {
				select {
				case h := <-updates:
					webHandler.Hashring(h)
				case <-cancel:
					return nil
				}
				select {
				// If any new hashring is received, then mark the handler as unready, but keep it alive.
				case <-updates:
					webHandler.Hashring(nil)
					level.Info(logger).Log("msg", "hashring has changed; server is not ready to receive web requests.")
				case <-cancel:
					return nil
				}
				<-cancel
				return nil
			},
			func(err error) {
				close(cancel)
			},
		)
	}

	level.Debug(logger).Log("msg", "setting up metric http listen-group")
	if err := metricHTTPListenGroup(g, logger, reg, httpMetricsBindAddr); err != nil {
		return err
	}

	level.Debug(logger).Log("msg", "setting up grpc server")
	{
		var (
			s   *grpc.Server
			l   net.Listener
			err error
		)
		g.Add(func() error {
			<-dbOpen

			l, err = net.Listen("tcp", grpcBindAddr)
			if err != nil {
				return errors.Wrap(err, "listen API address")
			}

			db := localStorage.Get()
			tsdbStore := store.NewTSDBStore(log.With(logger, "component", "thanos-tsdb-store"), reg, db, component.Receive, lset)

			opts, err := defaultGRPCServerOpts(logger, reg, tracer, cert, key, clientCA)
			if err != nil {
				return errors.Wrap(err, "setup gRPC server")
			}
			s = grpc.NewServer(opts...)
			storepb.RegisterStoreServer(s, tsdbStore)

			level.Info(logger).Log("msg", "listening for StoreAPI gRPC", "address", grpcBindAddr)
			return errors.Wrap(s.Serve(l), "serve gRPC")
		}, func(error) {
			if s != nil {
				s.Stop()
			}
		})
	}

	level.Debug(logger).Log("msg", "setting up receive http handler")
	{
		g.Add(
			func() error {
				return errors.Wrap(webHandler.Run(), "error starting web server")
			},
			func(err error) {
				webHandler.Close()
			},
		)
	}

	confContentYaml, err := objStoreConfig.Content()
	if err != nil {
		return err
	}

	upload := true
	if len(confContentYaml) == 0 {
		level.Info(logger).Log("msg", "No supported bucket was configured, uploads will be disabled")
		upload = false
	}

	if upload {
		// The background shipper continuously scans the data directory and uploads
		// new blocks to Google Cloud Storage or an S3-compatible storage service.
		bkt, err := client.NewBucket(logger, confContentYaml, reg, component.Sidecar.String())
		if err != nil {
			return err
		}

		// Ensure we close up everything properly.
		defer func() {
			if err != nil {
				runutil.CloseWithLogOnErr(logger, bkt, "bucket client")
			}
		}()

		s := shipper.New(logger, reg, dataDir, bkt, func() labels.Labels { return lset }, metadata.ReceiveSource)

		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			defer runutil.CloseWithLogOnErr(logger, bkt, "bucket client")

			return runutil.Repeat(30*time.Second, ctx.Done(), func() error {
				if uploaded, err := s.Sync(ctx); err != nil {
					level.Warn(logger).Log("err", err, "uploaded", uploaded)
				}

				return nil
			})
		}, func(error) {
			cancel()
		})
	}

	level.Info(logger).Log("msg", "starting receiver")

	return nil
}
