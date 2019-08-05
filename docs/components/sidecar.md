---
title: Sidecar
type: docs
menu: components
---

# Sidecar

The sidecar component of Thanos gets deployed along with a Prometheus instance. This allows sidecar to optionally upload metrics to object storage and allow [Queriers](./query.md) to query Prometheus data with common, efficient StoreAPI.

In details: 

* It implements Thanos' Store API on top of Prometheus' remote-read API. This allows [Queriers](./query.md) to treat Prometheus servers as yet another source of time series data without directly talking to its APIs.
* Optionally, the sidecar uploads TSDB blocks to an object storage bucket as Prometheus produces them every 2 hours. This allows Prometheus servers to be run with relatively low retention while their historic data is made durable and queryable via object storage.

  NOTE: This still does NOT mean that Prometheus can be fully stateless, because if it crashes and restarts you will lose ~2 hours of metrics, so persistent disk for Prometheus is highly recommended. The closest to stateless you can get is using remote write (which Thanos experimentally supports, see [this](../proposals/approved/201812_thanos-remote-receive.md). Remote write has other risks and consequences, and still if crashed you loose in positive case seconds of metrics data, so persistent disk is recommended in all cases. 

* Optionally Thanos sidecar is able to watch Prometheus rules and configuration, decompress and substitute environment variables if needed and ping Prometheus to reload them. Read more about this in [here](./sidecar.md#reloader-configuration)


Prometheus servers connected to the Thanos cluster via the sidecar are subject to a few limitations and recommendations for safe operations:

* The recommended Prometheus version is 2.2.1 or greater (including newest releases). This is due to Prometheus instability in previous versions as well as lack of `flags` endpoint.
* (!) The Prometheus `external_labels` section of the Prometheus configuration file has unique labels in the overall Thanos system. Those external labels will be used by sidecar and then Thanos in many places:
  
  * [Querier](./query.md) to filter out store APIs to touch during query requests.
  * Many object storage readers like [compactor](./compact.md) and [store gateway](./store.md) which groups the blocks by Prometheus source. Each produced TSDB block by Prometheus is labelled with external label by sidecar before upload to object storage.
  
* The `--web.enable-lifecycle` flag is enabled if you want to use sidecar reloading features (`--reload.*` flags).

If you choose to use the sidecar to also upload to object storage:

* The `--storage.tsdb.min-block-duration` and `--storage.tsdb.max-block-duration` must be set to equal values to disable local compaction on order to use Thanos sidecar upload, otherwise leave local compaction on if sidecar just exposes StoreAPI and your retention is normal. The default of `2h` is recommended. 
  Mentioned parameters set to equal values disable the internal Prometheus compaction, which is needed to avoid the uploaded data corruption when Thanos compactor does its job, this is critical for data consistency and should not be ignored if you plan to use Thanos compactor. Even though you set mentioned parameters equal, you might observe Prometheus internal metric `prometheus_tsdb_compactions_total` being incremented, don't be confused by that: Prometheus writes initial head block to filesytem via internal compaction mechanism, but if you have followed recommendations - data won't be modified by Prometheus before sidecar uploads it. Thanos sidecar will also check sanity of the flags set to Prometheus on the startup and log errors or warning if they have been configured improperly (#838).
* The retention is recommended to not be lower than three times the min block duration, so 6 hours. This achieves resilience in the face of connectivity issues to the object storage since all local data will remain available within the Thanos cluster. If connectivity gets restored the backlog of blocks gets uploaded to the object storage.

## Reloader Configuration

Thanos can watch changes in Prometheus configuration and refresh Prometheus configuration if `--web.enable-lifecycle` enabled.

You can configure watching for changes in directory via `--reloader.rule-dir=DIR_NAME` flag.

Thanos sidecar can watch `--reloader.config-file=CONFIG_FILE` configuration file, evaluate environment variables found in there and produce generated config in `--reloader.config-envsubst-file=OUT_CONFIG_FILE` file.


## Example basic deployment

```bash
$ prometheus \
  --storage.tsdb.max-block-duration=2h \
  --storage.tsdb.min-block-duration=2h \
  --web.enable-lifecycle
```

```bash
$ thanos sidecar \
    --tsdb.path        "/path/to/prometheus/data/dir" \
    --prometheus.url   "http://localhost:9090" \
    --objstore.config-file  "bucket.yml"
```

The example content of `bucket.yml`:

```yaml
type: GCS
config:
  bucket: example-bucket
```

## Flags

[embedmd]:# (flags/sidecar.txt $)
```$
usage: thanos sidecar [<flags>]

sidecar for Prometheus server

Flags:
  -h, --help                     Show context-sensitive help (also try
                                 --help-long and --help-man).
      --version                  Show application version.
      --log.level=info           Log filtering level.
      --log.format=logfmt        Log format to use.
      --tracing.config-file=<tracing.config-yaml-path>
                                 Path to YAML file that contains tracing
                                 configuration.
      --tracing.config=<tracing.config-yaml>
                                 Alternative to 'tracing.config-file' flag.
                                 Tracing configuration in YAML.
      --http-address="0.0.0.0:10902"
                                 Listen host:port for HTTP endpoints.
      --grpc-address="0.0.0.0:10901"
                                 Listen ip:port address for gRPC endpoints
                                 (StoreAPI). Make sure this address is routable
                                 from other components.
      --grpc-server-tls-cert=""  TLS Certificate for gRPC server, leave blank to
                                 disable TLS
      --grpc-server-tls-key=""   TLS Key for the gRPC server, leave blank to
                                 disable TLS
      --grpc-server-tls-client-ca=""
                                 TLS CA to verify clients against. If no client
                                 CA is specified, there is no client
                                 verification on server side. (tls.NoClientCert)
      --prometheus.url=http://localhost:9090
                                 URL at which to reach Prometheus's API. For
                                 better performance use local network.
      --tsdb.path="./data"       Data directory of TSDB.
      --reloader.config-file=""  Config file watched by the reloader.
      --reloader.config-envsubst-file=""
                                 Output file for environment variable
                                 substituted config file.
      --reloader.rule-dir=RELOADER.RULE-DIR ...
                                 Rule directories for the reloader to refresh
                                 (repeated field).
      --objstore.config-file=<bucket.config-yaml-path>
                                 Path to YAML file that contains object store
                                 configuration.
      --objstore.config=<bucket.config-yaml>
                                 Alternative to 'objstore.config-file' flag.
                                 Object store configuration in YAML.

```
