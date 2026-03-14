# examples

This module contains the runnable demos, framework compatibility tests, and benchmark drivers for `github.com/DevNewbie1826/http-over-polling`.

The root module is the core library. This `examples/` module exists so you can:

- run comparable stdlib vs hop servers
- inspect idle-connection and WebSocket goroutine behavior
- verify framework compatibility without touching application code
- reproduce the bundled `wrk` benchmark workflow

## Why this is a separate module

`examples/` depends on the root module like a normal downstream consumer.

It exists so the examples, benchmarks, and compatibility checks exercise the published core module shape instead of being mixed into the main library module.

## What is inside

| Path | Purpose |
|---|---|
| `cmd/stdhttp_example` | Baseline stdlib HTTP server |
| `cmd/netpoll_example` | Comparable hop-based HTTP server |
| `cmd/ws_example` | WebSocket and SSE example server |
| `cmd/benchparity` | Shared response bodies/constants for fair comparisons |
| `integration/framework_compat_test.go` | `chi`, `gin`, `echo` compatibility checks |
| `integration/wsdemo_test.go` | End-to-end WebSocket and SSE behavior checks |
| `integration/wsdemo_bench_test.go` | Goroutine budget benchmarks |
| `internal/wsapp` | Shared server code used by the examples and integration tests |
| `scripts/wrk_compare.sh` | Built-binary `wrk` comparison helper |

## Quick start

Run the full examples test suite:

```bash
go test ./... -count=1
```

Run the main example servers:

```bash
go run ./cmd/stdhttp_example
go run ./cmd/netpoll_example
go run ./cmd/ws_example
```

## Example servers

### `stdhttp_example`

A plain stdlib baseline.

Use it when you want to compare:

- standard `net/http` request handling
- goroutine-per-connection behavior on idle HTTP
- process-level `wrk` throughput against the hop path

Run:

```bash
go run ./cmd/stdhttp_example
```

Then benchmark it manually:

```bash
wrk -t6 -c300 -d30s http://localhost:8080
```

### `netpoll_example`

A comparable server built on:

```go
hop.ListenAndServe(":8080", handler)
```

The handler logic is intentionally simple so the comparison is about the serving stack, not application code.

Run:

```bash
go run ./cmd/netpoll_example
```

Benchmark it manually:

```bash
wrk -t6 -c300 -d30s http://localhost:8080
```

### `ws_example`

A demo server for showing the difference between legacy and poller-driven connection handling.

Run:

```bash
go run ./cmd/ws_example
```

Available endpoints:

| Endpoint | What it shows |
|---|---|
| `/ws-gobwas-low` | Low-level event-driven WebSocket handling |
| `/ws-gobwas-high` | `wsutil`-based event-driven WebSocket handling |
| `/ws-gorilla-std` | Classic Gorilla goroutine loop |
| `/ws-gorilla-event` | Gorilla API with poller-driven read dispatch |
| `/sse` | Hijack-based SSE example |
| `/file` | Simple file serving |

This example is useful when you want to demonstrate:

- legacy WebSocket behavior
- poller WebSocket behavior
- goroutine-heavy vs goroutine-flat connection styles

## Framework compatibility

The compatibility tests exist to prove that existing `net/http`-style application code does not need to be rewritten just to try the hop transport path.

Currently covered:

- `chi`
- `gin`
- `echo`

Run all framework checks:

```bash
go test ./integration -run 'TestChiCompatibility|TestGinCompatibility|TestEchoCompatibility' -count=1
```

## Goroutine budget benchmark

The goroutine benchmark is the easiest way to show why the poller path matters.

Run the default matrix:

```bash
go test ./integration -run '^$' -bench WebSocketServerGoroutines -benchmem -count=1
```

Increase connection count:

```bash
WSDEMO_CONN_COUNT=128 go test ./integration -run '^$' -bench WebSocketServerGoroutines -benchmem -count=1
```

For larger one-shot headline measurements:

```bash
WSDEMO_CONN_COUNT=4096 go test ./integration -run '^$' -bench 'BenchmarkWebSocketServerGoroutines/std-idle-http$' -benchmem -benchtime=1x -count=1
```

What this benchmark helps answer:

| Question | Relevant result |
|---|---|
| Do idle stdlib connections scale with goroutine count? | `std-idle-http` |
| Does the hop idle path stay event-driven? | `hop-idle-http` |
| How expensive is legacy Gorilla handling? | `hop-gorilla-std` |
| Do poller WebSocket paths stay flat? | `hop-gobwas-*`, `hop-gorilla-event` |

## `wrk` benchmark workflow

The bundled script exists to compare the stdlib example server and the hop example server with the same request pattern.

Run it:

```bash
./scripts/wrk_compare.sh
```

Default command shape:

```bash
wrk -t6 -c300 -d30s http://localhost:8080
```

What the script does:

1. builds `stdhttp_example` and `netpoll_example`
2. starts one server at a time
3. runs `wrk`
4. stores raw output under `.wrk-results/`
5. prints a summary table from the captured runs

Useful knobs:

| Variable | Meaning | Default |
|---|---|---|
| `THREADS` | `wrk` thread count | `6` |
| `CONNECTIONS` | concurrent connections | `300` |
| `DURATION` | benchmark duration | `30s` |
| `RUNS` | repeated measured runs | `3` |
| `WARMUPS` | warmup runs before measurement | `0` |
| `HOST` | benchmark host | `localhost` |
| `PORT` | server port | `8080` |
| `WITH_LATENCY` | add `--latency` to `wrk` | `0` |
| `SERVER_LIFETIME` | `per-kind` or `per-run` | `per-kind` |

Examples:

```bash
./scripts/wrk_compare.sh
RUNS=5 ./scripts/wrk_compare.sh
WITH_LATENCY=1 ./scripts/wrk_compare.sh
THREADS=8 CONNECTIONS=512 DURATION=60s ./scripts/wrk_compare.sh
```

When to use `WITH_LATENCY=1`:

- when you want percentile output for a separate latency-oriented pass
- not when you want your cleanest throughput headline number

## In-memory and parser-focused benches

The examples module is not the only place to benchmark the project. The root module still contains lower-level parser benches that are useful when you want to isolate hot-path behavior from process scheduling and socket state.

From the repo root:

```bash
go test ./internal/parser -run '^$' -bench . -benchmem
go test ./internal/parser -run '^$' -bench 'BenchmarkParserHttparserBenchmarkFixture$|BenchmarkNetHTTPHttparserBenchmarkFixture$' -benchmem -count=5
```

## Suggested workflow

If you are new to the repo, the easiest order is:

1. run `go test ./... -count=1`
2. run `go run ./cmd/stdhttp_example`
3. run `go run ./cmd/netpoll_example`
4. compare them with `wrk`
5. run `go run ./cmd/ws_example`
6. run the goroutine benchmark

That sequence gives you:

- correctness
- throughput comparison
- compatibility proof
- goroutine evidence
- WebSocket behavior examples
