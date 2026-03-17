# http-over-polling

[![CI](https://github.com/DevNewbie1826/http-over-polling/actions/workflows/ci.yml/badge.svg)](https://github.com/DevNewbie1826/http-over-polling/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/DevNewbie1826/http-over-polling/graph/badge.svg?token=YBSXRMKKIR)](https://codecov.io/gh/DevNewbie1826/http-over-polling)
[![Go Report Card](https://goreportcard.com/badge/github.com/DevNewbie1826/http-over-polling)](https://goreportcard.com/report/github.com/DevNewbie1826/http-over-polling)
[![Go Reference](https://pkg.go.dev/badge/github.com/DevNewbie1826/http-over-polling.svg)](https://pkg.go.dev/github.com/DevNewbie1826/http-over-polling)
[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

`http-over-polling` is a `net/http`-compatible experiment built on top of `cloudwego/netpoll`.

The current tree focuses on two things:

- serving regular `net/http` handlers through an event-loop-driven transport
- showing where poller-style handling helps, and where standard `net/http` remains competitive

The repository is strongest as a runnable playground: it ships a single example server, in-process benchmarks, and manual `wrk` reproduction steps.

> Platform note: the current transport path and examples are intended for Linux and macOS. Windows is not a supported target right now.

## At a glance

Latest local benchmark snapshot:

| Category | `std` | `hop` | Takeaway |
|---|---:|---:|---|
| `wrk` throughput | `218,662.67 req/s` | `223,689.11 req/s` | `hop` led by about `2.30%` on the bundled `/` workload |
| `wrk` avg latency | `1.15 ms` | `1.05 ms` | `hop` averaged lower latency in this run |
| `wrk` p99 latency | `6.02 ms` | `5.81 ms` | tail latency was slightly lower on `hop` in this run |
| HTTP idle connections | `10000` connections / `10000` goroutines | `10000` connections / `0 observed` goroutines | `std` grows with connections; `hop` stayed near zero in this run |
| WebSocket idle connections | `10000` connections / `10000` goroutines | `10000` connections / `0 observed` goroutines | the event-driven `hop` WS path stayed near zero in this run |

The `wrk` rows summarize the bundled `/` workload. The goroutine rows are process-wide observed deltas from the in-repo goroutine benchmarks, not universal guarantees. The exact commands and caveats are documented in `Benchmarking with wrk` and `In-process benchmarks`.

## What is in this repo

| Path | Role |
|---|---|
| `server/` | top-level server wrapper around `cloudwego/netpoll` |
| `engine/` | request execution pipeline and connection state management |
| `adaptor/` | `http.ResponseWriter` implementation plus hijack/read-handler bridge |
| `appcontext/` | request-scoped connection context helpers |
| `internal/parser/` | internal HTTP parsing implementation |
| `examples/` | runnable server plus benchmark and example tests |

## Quick start

Run the bundled example server from the repository root:

```bash
go run ./examples -type hop
```

Available modes:

| Mode | Command | Notes |
|---|---|---|
| `hop` | `go run ./examples -type hop` | `netpoll`-backed server |
| `std` | `go run ./examples -type std` | standard `net/http` baseline |

The example server listens on `:1826` and also starts `pprof` on `localhost:6060`.

## Example endpoints

`examples/main.go` exposes these routes:

| Endpoint | Purpose |
|---|---|
| `/` | overview page listing the demo endpoints |
| `/ws-gobwas-low` | low-level event-driven WebSocket echo |
| `/ws-gobwas-high` | `wsutil`-based event-driven WebSocket echo |
| `/ws-gorilla-std` | classic Gorilla loop with a goroutine per connection |
| `/ws-gorilla-event` | Gorilla API driven through `SetReadHandler` |
| `/sse` | simple server-sent events stream |
| `/file` | serves `examples/main.go` |

The WebSocket examples intentionally show different execution styles, not just different libraries. `gobwas` and the event-driven Gorilla path stay close to the poller model; the classic Gorilla loop demonstrates the more traditional goroutine-per-connection shape.

## Current API shape

There is no `hop.ListenAndServe(...)` convenience entrypoint in the current tree. The public setup pattern is explicit:

```go
package main

import (
    "log"
    "net/http"

    hengine "github.com/DevNewbie1826/http-over-polling/engine"
    hserver "github.com/DevNewbie1826/http-over-polling/server"
)

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("hello from hop"))
    })

    eng := hengine.NewEngine(mux)
    srv := hserver.NewServer(eng)

    log.Fatal(srv.Serve(":1826"))
}
```

Useful server options live in `server/server.go`:

- `server.WithPollerNum(...)`
- `server.WithBufferSize(...)`
- `server.WithMaxConns(...)`
- `server.WithKeepAliveTimeout(...)`
- `server.WithReadTimeout(...)`
- `server.WithWriteTimeout(...)`

For upgraded or hijacked flows, `adaptor.Hijacker` exposes `SetReadHandler(...)`, which is how the event-driven WebSocket examples plug custom frame handling into the connection lifecycle.

## Benchmarking with `wrk`

The repository does not currently ship a `wrk_compare.sh` script. The supported flow is manual:

1. start one server mode
2. warm it up with a short `wrk` run
3. measure it with the same `wrk` command
4. restart in the other mode and repeat

Command shape used for the measurements below:

```bash
wrk -t6 -c300 -d30s --latency http://127.0.0.1:1826/
```

Metric guide:

- `Requests/sec`: completed requests per second
- `Latency`: request-to-response time; `--latency` adds percentile output
- `Transfer/sec`: response bytes transferred per second

`wrk`'s own documentation recommends using a meaningful test duration and reading percentile output rather than only the average. These numbers were taken locally on the same machine as the server, so treat them as directional measurements for this workload, not universal claims.

This comparison is for the bundled example configurations, not a transport-only micro-comparison. In the current tree, `hop` runs through `server.NewServer(...)` with explicit `WithPollerNum`, `WithBufferSize`, and zero timeout settings, and the server path also uses the listener configuration in `server/server.go`. The `std` baseline is the plain `http.ListenAndServe(...)` path from `examples/main.go`.

### Latest local `wrk` result

Measured on 2026-03-17 against the `/` handler in `examples/main.go`, after a 5 second warm-up for each mode.

| Metric | `std` | `hop` | Delta (`hop` vs `std`) |
|---|---:|---:|---:|
| Requests/sec | 218,662.67 | 223,689.11 | **+2.30%** |
| Transfer/sec | 95.51 MB/s | 97.70 MB/s | **+2.29%** |
| Avg latency | 1.15 ms | 1.05 ms | **-8.70%** |
| P50 latency | 711 us | 722 us | **+1.55%** |
| P99 latency | 6.02 ms | 5.81 ms | **-3.49%** |

What this current measurement says:

- on the bundled root-handler workload, `hop` now edges out `std` in throughput
- the gap is modest, so the interesting part is not a huge headline multiplier
- the stronger story in this repository is still execution model control, especially for upgraded or long-lived connections
- percentile results are mixed: `hop` improves average and `p99` here, while `p50` is effectively flat to slightly worse

If you want to rerun it yourself:

```bash
# std
go run ./examples -type std
wrk -t6 -c300 -d5s http://127.0.0.1:1826/
wrk -t6 -c300 -d30s --latency http://127.0.0.1:1826/

# hop
go run ./examples -type hop
wrk -t6 -c300 -d5s http://127.0.0.1:1826/
wrk -t6 -c300 -d30s --latency http://127.0.0.1:1826/
```

## In-process benchmarks

For faster iteration, the example package also includes Go benchmarks:

```bash
go test ./examples -run '^$' -bench '^BenchmarkServer_(Standard|Hop)_HTTP$|^BenchmarkServer_Standard_WS$' -benchmem -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Standard_HTTP_Goroutines$' -benchmem -benchtime=1x -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Hop_HTTP_Goroutines$' -benchmem -benchtime=1x -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Standard_WS_Goroutines$' -benchmem -benchtime=1x -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Hop_WS_Goroutines$' -benchmem -benchtime=1x -count=1
```

The goroutine benchmarks open as many idle connections as the current process can sustain for that benchmark run. By default they derive a target from the open-file limit and the local ephemeral port range, then report the number of connections actually established before sampling. You can still override the target explicitly with `GOROUTINE_BENCH_CONN_COUNT`.

The custom metrics mean:

- `connections`: idle connections successfully established for that run
- `goroutines`: peak additional goroutines observed above the post-startup baseline during sampling
- `goroutines/conn`: observed goroutine delta per idle connection

Use `GOROUTINE_BENCH_CONN_COUNT` to change the connection count:

```bash
GOROUTINE_BENCH_CONN_COUNT=512 go test ./examples -run '^$' -bench '^BenchmarkServer_Standard_HTTP_Goroutines$' -benchmem -benchtime=1x -count=1
GOROUTINE_BENCH_CONN_COUNT=512 go test ./examples -run '^$' -bench '^BenchmarkServer_Hop_WS_Goroutines$' -benchmem -benchtime=1x -count=1
```

A recent local run of the throughput-focused in-process benchmarks produced:

| Benchmark | Result |
|---|---:|
| `BenchmarkServer_Standard_HTTP` | `27,329 ns/op`, `5115 B/op`, `63 allocs/op` |
| `BenchmarkServer_Hop_HTTP` | `32,912 ns/op`, `4852 B/op`, `62 allocs/op` |
| `BenchmarkServer_Standard_WS` | `13,352 ns/op`, `1088 B/op`, `5 allocs/op` |

That is useful context: the in-process benchmark and the external `wrk` benchmark answer different questions. If they disagree, prefer describing the workload rather than forcing one universal conclusion.

A recent local run of the goroutine benchmarks, using the automatically derived maximum connection count for each individual benchmark run on this machine, produced:

| Benchmark | Result |
|---|---:|
| `BenchmarkServer_Standard_HTTP_Goroutines` | `2865 connections`, `2865 goroutines`, `1.000 goroutines/conn` |
| `BenchmarkServer_Hop_HTTP_Goroutines` | `8123 connections`, `0 observed goroutines`, `0 goroutines/conn` |
| `BenchmarkServer_Standard_WS_Goroutines` | `987 connections`, `987 goroutines`, `1.000 goroutines/conn` |
| `BenchmarkServer_Hop_WS_Goroutines` | `4382 connections`, `0 observed goroutines`, `0 goroutines/conn` |

Those goroutine benchmarks are intentionally about idle connection shape, not request throughput. They are most useful as sanity checks for per-connection goroutine growth, and the `hop` values should be read as “near-zero additional goroutines observed in this run”, not as a universal structural guarantee.

## Verification commands

Core verification:

```bash
go test ./... -count=1
```

Targeted example checks:

```bash
go test ./examples -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_(Standard|Hop)_HTTP$|^BenchmarkServer_Standard_WS$' -benchmem -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Standard_HTTP_Goroutines$' -benchmem -benchtime=1x -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Hop_HTTP_Goroutines$' -benchmem -benchtime=1x -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Standard_WS_Goroutines$' -benchmem -benchtime=1x -count=1
go test ./examples -run '^$' -bench '^BenchmarkServer_Hop_WS_Goroutines$' -benchmem -benchtime=1x -count=1
```

If you are investigating the parser directly:

```bash
go test ./internal/parser -run '^$' -bench . -benchmem
```

## How to describe this project honestly

The current repository is strongest when described this way:

- it adapts `net/http` handlers onto a `cloudwego/netpoll`-backed server path
- it includes side-by-side stdlib and poller-driven server modes in one runnable example
- it demonstrates multiple upgraded-connection styles, including event-driven WebSocket handling
- it gives you enough local commands to rerun both in-process and end-to-end measurements yourself

What to avoid saying:

- `hop` is always faster than `net/http`
- `netpoll` removes goroutines entirely
- one benchmark here predicts every production workload

That narrower framing matches both the current code and the upstream `netpoll` positioning more closely.

## Acknowledgements

This project references and learns from the ideas and implementations in:

- `https://github.com/cloudwego/netpoll`
- `https://github.com/valyala/bytebufferpool`
- `https://github.com/valyala/fasthttp/tree/master/tcplisten`

Thanks to the authors and maintainers of those projects.
