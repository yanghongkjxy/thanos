<p align="center"><img src="docs/img/Thanos-logo_fullmedium.png" alt="Thanos Logo"></p>

[![CircleCI](https://circleci.com/gh/thanos-io/thanos.svg?style=svg)](https://circleci.com/gh/thanos-io/thanos)
[![Go Report Card](https://goreportcard.com/badge/github.com/thanos-io/thanos)](https://goreportcard.com/report/github.com/thanos-io/thanos)
[![GoDoc](https://godoc.org/github.com/thanos-io/thanos?status.svg)](https://godoc.org/github.com/thanos-io/thanos)
[![Slack](https://img.shields.io/badge/join%20slack-%23thanos-brightgreen.svg)](https://slack.cncf.io/)
[![Netlify Status](https://api.netlify.com/api/v1/badges/664a5091-934c-4b0e-a7b6-bc12f822a590/deploy-status)](https://app.netlify.com/sites/thanos-io/deploys)

## Overview

Thanos is a set of components that can be composed into a highly available metric
system with unlimited storage capacity, which can be added seamlessly on top of existing
Prometheus deployments.

Thanos leverages the Prometheus 2.0 storage format to cost-efficiently store historical metric
data in any object storage while retaining fast query latencies. Additionally, it provides
a global query view across all Prometheus installations and can merge data from Prometheus
HA pairs on the fly.

Concretely the aims of the project are:

1. Global query view of metrics.
1. Unlimited retention of metrics.
1. High availability of components, including Prometheus.

## Architecture Overview

![architecture_overview](docs/img/arch.jpg)

## Getting Started

* **[Getting Started](https://thanos.io/getting-started.md/)**
* [Design](https://thanos.io/design.md/)
* [Prom Meetup Slides](https://www.slideshare.net/BartomiejPotka/thanos-global-durable-prometheus-monitoring)
* [Introduction blog post](https://improbable.io/games/blog/thanos-prometheus-at-scale)
* [Benchmarks](https://github.com/thanos-io/thanos/tree/master/benchmark)
* [Proposals](docs/proposals)
* [Integrations](docs/integrations.md)

## Features

* Global querying view across all connected Prometheus servers
* Deduplication and merging of metrics collected from Prometheus HA pairs
* Seamless integration with existing Prometheus setups
* Any object storage as its only, optional dependency
* Downsampling historical data for massive query speedup
* Cross-cluster federation
* Fault-tolerant query routing
* Simple gRPC "Store API" for unified data access across all metric data
* Easy integration points for custom metric providers

## Thanos Philosophy

The philosophy of Thanos and our community is borrowing much from UNIX philosophy and the golang programming language.

* Each sub command should do one thing and do it well
  * eg. thanos query proxies incoming calls to known store API endpoints merging the result
* Write components that work together
  * e.g. blocks should be stored in native prometheus format
* Make it easy to read, write, and, run components
  * e.g. reduce complexity in system design and implementation

## Releases

Master should be stable and usable. Every commit to master builds docker image named `master-<data>-<sha>`.

We also perform minor releases every 6 weeks. 
During that, we build tarballs for major platforms and docker image.

See [release process docs](docs/release-process.md) for details.

## Contributing

Contributions are very welcome! See our [CONTRIBUTING.md](CONTRIBUTING.md) for more information.

## Community

Thanos is an open source project and we value and welcome new contributors and members
of the community. Here are ways to get in touch with the community:

* Slack: [#thanos](https://slack.cncf.io/)
* Issue Tracker: [GitHub Issues](https://github.com/thanos-io/thanos/issues)

## Maintainers

See [MAINTAINERS.md](MAINTAINERS.md)
