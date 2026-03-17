package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	hengine "github.com/DevNewbie1826/http-over-polling/engine"
	hserver "github.com/DevNewbie1826/http-over-polling/server"
	"github.com/gorilla/websocket"
)

const goroutineBenchFDReserve = 64
const goroutineBenchInfiniteRlimitThreshold = 1 << 62
const goroutineBenchPortReserve = 1024
const defaultEphemeralPortFirst = 49152
const defaultEphemeralPortLast = 65535

type benchmarkServer struct {
	addr     string
	shutdown func(context.Context) error
}

// --- Benchmarks ---

func getBenchmarkClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        2000,
			MaxIdleConnsPerHost: 2000,
			IdleConnTimeout:     30 * time.Second,
			DisableKeepAlives:   false,
		},
	}
}

// Handlers

func benchmarkRootHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello Benchmark"))
}

func benchmarkStdWSHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		// Echo back
		if err := conn.WriteMessage(mt, msg); err != nil {
			return
		}
	}
}

func goroutineBenchmarkConnCountFromRlimit(limit uint64) int {
	if limit >= goroutineBenchInfiniteRlimitThreshold {
		return 0
	}
	if limit <= goroutineBenchFDReserve*2 {
		return 1
	}
	return int((limit - goroutineBenchFDReserve) / 2)
}

func goroutineBenchmarkConnCountFromPortRange(first, last uint32) int {
	if last <= first {
		return 1
	}
	size := int(last-first) + 1 - goroutineBenchPortReserve
	if size < 1 {
		return 1
	}
	return size
}

func goroutineBenchmarkPortRange() (uint32, uint32) {
	raw, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) == 2 {
			first, errFirst := strconv.ParseUint(fields[0], 10, 32)
			last, errLast := strconv.ParseUint(fields[1], 10, 32)
			if errFirst == nil && errLast == nil {
				return uint32(first), uint32(last)
			}
		}
	}

	return defaultEphemeralPortFirst, defaultEphemeralPortLast
}

func goroutineBenchmarkConnCount(tb testing.TB) (int, bool) {
	tb.Helper()

	raw := os.Getenv("GOROUTINE_BENCH_CONN_COUNT")
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			tb.Fatalf("invalid GOROUTINE_BENCH_CONN_COUNT %q", raw)
		}
		return n, true
	}

	portConnCount := 0
	first, last := goroutineBenchmarkPortRange()
	portConnCount = goroutineBenchmarkConnCountFromPortRange(first, last)

	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err == nil {
		if lim.Cur < lim.Max {
			lim.Cur = lim.Max
			_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
			_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim)
		}
		fdConnCount := goroutineBenchmarkConnCountFromRlimit(lim.Cur)
		if fdConnCount == 0 {
			if portConnCount > 0 {
				return portConnCount, false
			}
			return 0, false
		}
		if portConnCount > 0 && portConnCount < fdConnCount {
			return portConnCount, false
		}
		return fdConnCount, false
	}

	if portConnCount > 0 {
		return portConnCount, false
	}
	return 0, false
}

func closeAll(closers []io.Closer) {
	for _, closer := range closers {
		if closer != nil {
			switch c := closer.(type) {
			case interface{ UnderlyingConn() net.Conn }:
				if tcpConn, ok := c.UnderlyingConn().(*net.TCPConn); ok {
					tcpConn.SetLinger(0)
				}
			case *net.TCPConn:
				c.SetLinger(0)
			}
			closer.Close()
		}
	}
}

func startStandardHTTPBenchmarkServer(tb testing.TB, handler http.Handler) *benchmarkServer {
	tb.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}

	srv := &http.Server{Handler: handler}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	return &benchmarkServer{
		addr: ln.Addr().String(),
		shutdown: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	}
}

func startHopHTTPBenchmarkServer(tb testing.TB, handler http.Handler) *benchmarkServer {
	tb.Helper()

	eng := hengine.NewEngine(handler)
	srv := hserver.NewServer(eng,
		hserver.WithReadTimeout(0),
		hserver.WithWriteTimeout(0),
		hserver.WithPollerNum(1),
		hserver.WithBufferSize(512),
	)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() {
		if err := srv.Serve(addr); err != nil {
			panic(err)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			tb.Fatalf("hop server did not start listening on %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	return &benchmarkServer{
		addr: addr,
		shutdown: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	}
}

func openIdleHTTPConnections(tb testing.TB, addr string, count int, strict bool) []io.Closer {
	tb.Helper()

	closers := make([]io.Closer, 0, count)
	for i := 0; count == 0 || i < count; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			if !strict && len(closers) > 0 {
				break
			}
			closeAll(closers)
			tb.Fatalf("Dial() error at connection %d/%d: %v", i+1, count, err)
		}

		if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: benchmark\r\nConnection: keep-alive\r\n\r\n"); err != nil {
			conn.Close()
			closeAll(closers)
			tb.Fatalf("WriteString() error at connection %d/%d: %v", i+1, count, err)
		}

		reader := bufio.NewReader(conn)
		req := &http.Request{Method: http.MethodGet}
		resp, err := http.ReadResponse(reader, req)
		if err != nil {
			conn.Close()
			closeAll(closers)
			tb.Fatalf("ReadResponse() error at connection %d/%d: %v", i+1, count, err)
		}

		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			resp.Body.Close()
			conn.Close()
			closeAll(closers)
			tb.Fatalf("Copy() error at connection %d/%d: %v", i+1, count, err)
		}
		resp.Body.Close()

		closers = append(closers, conn)
	}

	return closers
}

func openIdleWebSocketConnections(tb testing.TB, url string, count int, strict bool) []io.Closer {
	tb.Helper()

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	closers := make([]io.Closer, 0, count)
	for i := 0; count == 0 || i < count; i++ {
		conn, _, err := dialer.Dial(url, nil)
		if err != nil {
			if !strict && len(closers) > 0 {
				break
			}
			closeAll(closers)
			tb.Fatalf("Dial() error at connection %d/%d: %v", i+1, count, err)
		}
		closers = append(closers, conn)
	}

	return closers
}

func waitForPeakGoroutineDelta(baseline int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	peakDelta := 0
	stableCount := 0
	lastDelta := -1

	for {
		delta := runtime.NumGoroutine() - baseline
		if delta > peakDelta {
			peakDelta = delta
		}
		if delta == lastDelta {
			stableCount++
		} else {
			stableCount = 0
			lastDelta = delta
		}
		if stableCount >= 3 || time.Now().After(deadline) {
			return peakDelta
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func benchmarkIdleHTTPConnectionGoroutines(b *testing.B, startServer func(testing.TB, http.Handler) *benchmarkServer) {
	b.Helper()

	connCount, strictCount := goroutineBenchmarkConnCount(b)
	b.ReportAllocs()

	var totalDelta int
	var measuredConnCount int
	for i := 0; i < b.N; i++ {
		mux := http.NewServeMux()
		mux.HandleFunc("/", benchmarkRootHandler)

		srv := startServer(b, mux)
		baseline := runtime.NumGoroutine()

		conns := openIdleHTTPConnections(b, srv.addr, connCount, strictCount)
		measuredConnCount = len(conns)
		delta := waitForPeakGoroutineDelta(baseline, 750*time.Millisecond)
		totalDelta += delta

		closeAll(conns)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := srv.shutdown(ctx); err != nil {
			cancel()
			b.Fatalf("Shutdown() error: %v", err)
		}
		cancel()
	}

	avgDelta := float64(totalDelta) / float64(b.N)
	avgConnCount := float64(measuredConnCount)
	b.ReportMetric(avgDelta, "goroutines")
	b.ReportMetric(avgConnCount, "connections")
	b.ReportMetric(avgDelta/avgConnCount, "goroutines/conn")
}

func benchmarkIdleWSConnectionGoroutines(b *testing.B, startServer func(testing.TB, http.Handler) *benchmarkServer, handler http.HandlerFunc) {
	b.Helper()

	connCount, strictCount := goroutineBenchmarkConnCount(b)
	b.ReportAllocs()

	var totalDelta int
	var measuredConnCount int
	for i := 0; i < b.N; i++ {
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", handler)

		srv := startServer(b, mux)
		baseline := runtime.NumGoroutine()

		conns := openIdleWebSocketConnections(b, "ws://"+srv.addr+"/ws", connCount, strictCount)
		measuredConnCount = len(conns)
		delta := waitForPeakGoroutineDelta(baseline, 750*time.Millisecond)
		totalDelta += delta

		closeAll(conns)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := srv.shutdown(ctx); err != nil {
			cancel()
			b.Fatalf("Shutdown() error: %v", err)
		}
		cancel()
	}

	avgDelta := float64(totalDelta) / float64(b.N)
	avgConnCount := float64(measuredConnCount)
	b.ReportMetric(avgDelta, "goroutines")
	b.ReportMetric(avgConnCount, "connections")
	b.ReportMetric(avgDelta/avgConnCount, "goroutines/conn")
}

// HTTP Benchmarks

func BenchmarkServer_Standard_HTTP(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", benchmarkRootHandler)

	// Create Listener explicitly to safely get a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	client := getBenchmarkClient()
	url := "http://" + addr + "/"

	// Wait for server? (ln is already open)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		// CRITICAL: Read body to EOF to allow connection reuse (Keep-Alive)
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkServer_Standard_HTTP_Goroutines(b *testing.B) {
	benchmarkIdleHTTPConnectionGoroutines(b, startStandardHTTPBenchmarkServer)
}

func BenchmarkServer_Hop_HTTP(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", benchmarkRootHandler)

	eng := hengine.NewEngine(mux)
	srv := hserver.NewServer(eng,
		hserver.WithReadTimeout(0),
		hserver.WithWriteTimeout(0),
		hserver.WithPollerNum(1),
		hserver.WithBufferSize(512),
	)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	go func() {
		// Serve will bind again
		srv.Serve(addr)
	}()
	defer srv.Shutdown(context.Background())

	time.Sleep(200 * time.Millisecond)

	client := getBenchmarkClient()
	url := "http://" + addr + "/"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func BenchmarkServer_Hop_HTTP_Goroutines(b *testing.B) {
	benchmarkIdleHTTPConnectionGoroutines(b, startHopHTTPBenchmarkServer)
}

func BenchmarkServer_Standard_WS_Goroutines(b *testing.B) {
	benchmarkIdleWSConnectionGoroutines(b, startStandardHTTPBenchmarkServer, gorillaStdHandler)
}

func BenchmarkServer_Hop_WS_Goroutines(b *testing.B) {
	benchmarkIdleWSConnectionGoroutines(b, startHopHTTPBenchmarkServer, gorillaEventHandler)
}

// WebSocket Benchmarks

func BenchmarkServer_Standard_WS(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", benchmarkStdWSHandler)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	url := "ws://" + addr + "/ws"

	// Single Connection Throughput Test
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		b.Fatalf("Dial failed: %v", err)
	}
	defer c.Close()

	msg := []byte("Hello Benchmark")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			b.Fatal(err)
		}
		_, _, err := c.ReadMessage()
		if err != nil {
			b.Fatal(err)
		}
	}
}
