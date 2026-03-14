# http-over-polling

A `net/http`-compatible HTTP stack on top of `cloudwego/netpoll`.

If you want to keep normal handlers and framework code, but cut idle-connection goroutine growth and explore poller-driven WebSocket handling, this repo is built to show that with runnable examples and repeatable benchmarks.

## Why it stands out

| What you get | Why it matters |
|---|---|
| Idle HTTP without per-connection goroutines on the hop path | Better fit for large idle fan-out workloads |
| Poller-driven WebSocket examples | Avoid the usual legacy goroutine-per-connection shape |
| `wrk` comparison against stdlib server | Easy to verify throughput claims yourself |
| Framework compatibility examples | `chi`, `gin`, `echo` keep their normal handler model |
| In-memory benches plus process benches | You can inspect both hot paths and end-to-end behavior |

## At a glance

### Idle and WebSocket goroutine profile

Measured by `examples/integration/wsdemo_bench_test.go` at **4096 concurrent connections**.

| Scenario | Measured goroutines | Takeaway |
|---|---:|---|
| `std-idle-http` | 4096 | Idle stdlib path scales with connection count |
| `hop-idle-http` | 0 | Idle hop path stays event-driven |
| `std-gorilla-std` | 4096 | Standard WebSocket loop scales with connection count |
| `hop-gorilla-std` | 8190 | Legacy Gorilla loop on hop is effectively about 2 goroutines per connection |
| `hop-gobwas-low` | 0 | Poller-style path stays flat |
| `hop-gobwas-high` | 0 | Poller-style path stays flat |
| `hop-gorilla-event` | 0 | Event-driven Gorilla path stays flat |

What this means in practice:

| Style | Connection model |
|---|---|
| Legacy WebSocket loop | Roughly 2 goroutines per connection in the measured Gorilla legacy path |
| Poller WebSocket path | No per-connection goroutine growth in the measured scenario |

Run it yourself:

```bash
cd examples
go test ./integration -run '^$' -bench WebSocketServerGoroutines -benchmem -benchtime=1x -count=1
```

Increase the benchmark connection count:

```bash
cd examples
WSDEMO_CONN_COUNT=128 go test ./integration -run '^$' -bench WebSocketServerGoroutines -benchmem -count=1
```

### `wrk` result on the bundled example workload

Result source: `examples/.wrk-results/20260314-200547/`

Command shape:

```bash
wrk -t6 -c300 -d30s http://localhost:8080
```

| Metric | `stdhttp_example` | `netpoll_example` | Delta |
|---|---:|---:|---:|
| Requests/sec | 112,945.52 | 229,711.12 | **2.03x** |
| Transfer/sec | 15.62 MB/s | 31.77 MB/s | **2.03x** |
| Avg latency | 2.57 ms | 1.23 ms | **-52%** |

Headline takeaway:

| Statement | Value |
|---|---:|
| Throughput advantage on the bundled workload | **2.03x** |
| Additional requests/sec over stdlib | **+116,765.60 req/s** |
| Latency improvement | **1.34 ms lower avg latency** |

This is intentionally scoped evidence, not a blanket claim about every production workload.

## What you can run

### Example servers

| Example | Purpose |
|---|---|
| `examples/cmd/stdhttp_example` | Baseline stdlib server |
| `examples/cmd/netpoll_example` | Same simple workload on `hop.ListenAndServe(...)` |
| `examples/cmd/ws_example` | Legacy and poller WebSocket styles side by side |
| `examples/cmd/benchparity` | Shared response body/constants for fair comparisons |

Run them:

```bash
cd examples && go run ./cmd/stdhttp_example
cd examples && go run ./cmd/netpoll_example
cd examples && go run ./cmd/ws_example
```

### WebSocket example endpoints

| Endpoint | Mode |
|---|---|
| `/ws-gobwas-low` | Low-level event-driven |
| `/ws-gobwas-high` | `wsutil`-based event-driven |
| `/ws-gorilla-std` | Classic Gorilla goroutine loop |
| `/ws-gorilla-event` | Gorilla API on poller-driven read dispatch |
| `/sse` | Hijack-based SSE example |
| `/file` | File serving |

The point is not just that WebSockets work. The point is that you can see which styles keep the poller model and which styles fall back to legacy goroutine-heavy behavior.

## Framework compatibility

If you already use a normal `net/http` framework, the pitch is simple: keep your handler code, change the server path.

| Framework | Status |
|---|---|
| `chi` | Verified |
| `gin` | Verified |
| `echo` | Verified |

Run the compatibility tests:

```bash
cd examples
go test ./integration -run 'TestChiCompatibility|TestGinCompatibility|TestEchoCompatibility' -count=1
```

## In-memory benches are included too

This repo is not only about process-level demos.

| Benchmark type | Command |
|---|---|
| Parser benches | `go test ./internal/parser -run '^$' -bench . -benchmem` |
| Parser vs `net/http` | `go test ./internal/parser -run '^$' -bench 'BenchmarkParserHttparserBenchmarkFixture$|BenchmarkNetHTTPHttparserBenchmarkFixture$' -benchmem -count=5` |
| Goroutine budget | `cd examples && go test ./integration -run '^$' -bench WebSocketServerGoroutines -benchmem -count=1` |
| `wrk` comparison | `cd examples && ./scripts/wrk_compare.sh` |

These are useful for different questions:

| Question | Best benchmark |
|---|---|
| Is the parser path fast? | In-memory parser benches |
| Does the server stay event-driven under idle load? | Goroutine budget bench |
| Does the whole stack beat stdlib on the bundled workload? | `wrk` comparison |

## Public API

The main public entrypoint stays small:

```go
err := hop.ListenAndServe(":8080", handler)
```

Request path:

1. `transport.ListenAndServe(...)`
2. `transport.Events`
3. `hop.NewHttpConn(...)`
4. `internal/parser` request parsing
5. `http.Handler` execution

## Core layout

| Path | Role |
|---|---|
| `hop/` | `net/http`-compatible HTTP layer |
| `transport/` | netpoll-backed I/O and connection lifecycle |
| `internal/parser/` | parser implementation detail |
| `internal/tcplisten/` | listener helpers |
| `internal/bytebufferpool/` | buffer helper used by the core |
| `examples/` | demos, compatibility tests, benchmark drivers |

## Command index

### Go commands

| Goal | Command |
|---|---|
| Core test suite | `go test ./... -count=1` |
| Core vet | `go vet ./...` |
| Core race check | `go test -race ./transport ./hop` |
| Parser benches | `go test ./internal/parser -run '^$' -bench . -benchmem` |
| Parser vs `net/http` | `go test ./internal/parser -run '^$' -bench 'BenchmarkParserHttparserBenchmarkFixture$|BenchmarkNetHTTPHttparserBenchmarkFixture$' -benchmem -count=5` |
| Examples test suite | `cd examples && go test ./... -count=1` |
| 4096-connection goroutine bench | `cd examples && WSDEMO_CONN_COUNT=4096 go test ./integration -run '^$' -bench WebSocketServerGoroutines -benchmem -benchtime=1x -count=1` |
| Framework compatibility check | `cd examples && go test ./integration -run 'TestChiCompatibility|TestGinCompatibility|TestEchoCompatibility' -count=1` |

### `wrk` commands

| Goal | Command |
|---|---|
| Manual std baseline | `cd examples && go run ./cmd/stdhttp_example` then `wrk -t6 -c300 -d30s http://localhost:8080` |
| Manual hop run | `cd examples && go run ./cmd/netpoll_example` then `wrk -t6 -c300 -d30s http://localhost:8080` |
| Bundled comparison script | `cd examples && ./scripts/wrk_compare.sh` |
| Latency percentile pass | `cd examples && WITH_LATENCY=1 ./scripts/wrk_compare.sh` |

## Reproducing the headline result

If you want the same style of numbers shown above, use built binaries and the bundled script:

```bash
cd examples
./scripts/wrk_compare.sh
```

If you want manual runs instead:

```bash
cd examples && go run ./cmd/stdhttp_example
wrk -t6 -c300 -d30s http://localhost:8080

cd examples && go run ./cmd/netpoll_example
wrk -t6 -c300 -d30s http://localhost:8080
```

If you specifically want latency percentiles as a separate measurement pass:

```bash
cd examples
WITH_LATENCY=1 ./scripts/wrk_compare.sh
```

## Honest scope

This project is strongest when described this way:

- it shows a real goroutine-usage difference between legacy and poller-driven connection handling
- it ships a local `wrk` workload where the hop path can outperform the stdlib baseline
- it proves compatibility with existing `net/http` frameworks
- it gives you enough examples and benches to rerun the claims yourself

That is more persuasive than a generic "faster than net/http" slogan, because the evidence is shipped with the code.
