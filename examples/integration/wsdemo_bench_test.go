package integration_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	wsdemo "github.com/DevNewbie1826/http-over-polling/examples/internal/wsapp"
	"github.com/gorilla/websocket"
)

const defaultWebSocketBenchmarkConnCount = 24

func TestWebSocketBenchmarkConnCountFromEnv(t *testing.T) {
	t.Setenv("WSDEMO_CONN_COUNT", "128")

	if got := websocketBenchmarkConnCount(); got != 128 {
		t.Fatalf("websocketBenchmarkConnCount() = %d, want %d", got, 128)
	}

	t.Setenv("WSDEMO_CONN_COUNT", "")
	if got := websocketBenchmarkConnCount(); got != 24 {
		t.Fatalf("websocketBenchmarkConnCount() default = %d, want %d", got, 24)
	}
}

func TestWebSocketServerGoroutineBudget(t *testing.T) {
	check := func(t *testing.T, name string, fn func(*testing.T) int) {
		t.Helper()
		delta := fn(t)
		if delta < 0 {
			t.Fatalf("%s goroutine delta = %d, want non-negative", name, delta)
		}
	}

	check(t, "std-idle", func(t *testing.T) int {
		return measureIdleHTTPConnectionGoroutines(t, wsdemo.ServerKindStd, websocketBenchmarkConnCount())
	})
	check(t, "hop-idle", func(t *testing.T) int {
		return measureIdleHTTPConnectionGoroutines(t, wsdemo.ServerKindHop, websocketBenchmarkConnCount())
	})
	check(t, "std-websocket", func(t *testing.T) int {
		return measureWebSocketConnectionGoroutines(t, wsdemo.ServerKindStd, "/ws-gorilla-std", websocketBenchmarkConnCount())
	})
	check(t, "hop-websocket", func(t *testing.T) int {
		return measureWebSocketConnectionGoroutines(t, wsdemo.ServerKindHop, "/ws-gorilla-event", websocketBenchmarkConnCount())
	})
	check(t, "hop-websocket-legacy", func(t *testing.T) int {
		return measureWebSocketConnectionGoroutines(t, wsdemo.ServerKindHop, "/ws-gorilla-std", websocketBenchmarkConnCount())
	})
}

func BenchmarkWebSocketServerGoroutines(b *testing.B) {
	connCount := websocketBenchmarkConnCount()
	benchmarks := []struct {
		name string
		fn   func(*testing.B) int
	}{
		{name: "std-idle-http", fn: func(b *testing.B) int {
			return measureIdleHTTPConnectionGoroutinesBench(b, wsdemo.ServerKindStd, connCount)
		}},
		{name: "hop-idle-http", fn: func(b *testing.B) int {
			return measureIdleHTTPConnectionGoroutinesBench(b, wsdemo.ServerKindHop, connCount)
		}},
		{name: "std-gorilla-std", fn: func(b *testing.B) int {
			return measureWebSocketConnectionGoroutinesBench(b, wsdemo.ServerKindStd, "/ws-gorilla-std", connCount)
		}},
		{name: "hop-gobwas-low", fn: func(b *testing.B) int {
			return measureWebSocketConnectionGoroutinesBench(b, wsdemo.ServerKindHop, "/ws-gobwas-low", connCount)
		}},
		{name: "hop-gobwas-high", fn: func(b *testing.B) int {
			return measureWebSocketConnectionGoroutinesBench(b, wsdemo.ServerKindHop, "/ws-gobwas-high", connCount)
		}},
		{name: "hop-gorilla-std", fn: func(b *testing.B) int {
			return measureWebSocketConnectionGoroutinesBench(b, wsdemo.ServerKindHop, "/ws-gorilla-std", connCount)
		}},
		{name: "hop-gorilla-event", fn: func(b *testing.B) int {
			return measureWebSocketConnectionGoroutinesBench(b, wsdemo.ServerKindHop, "/ws-gorilla-event", connCount)
		}},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			var total int
			for i := 0; i < b.N; i++ {
				total += bm.fn(b)
			}
			avg := float64(total) / float64(b.N)
			b.ReportMetric(avg, "goroutines")
			b.ReportMetric(avg/float64(connCount), "goroutines/op")
		})
	}
}

func websocketBenchmarkConnCount() int {
	raw := os.Getenv("WSDEMO_CONN_COUNT")
	if raw == "" {
		return defaultWebSocketBenchmarkConnCount
	}
	connCount, err := strconv.Atoi(raw)
	if err != nil || connCount <= 0 {
		return defaultWebSocketBenchmarkConnCount
	}
	return connCount
}

func measureIdleHTTPConnectionGoroutinesBench(b *testing.B, kind wsdemo.ServerKind, connCount int) int {
	b.Helper()
	return measureIdleHTTPConnectionGoroutines(b, kind, connCount)
}

func measureWebSocketConnectionGoroutinesBench(b *testing.B, kind wsdemo.ServerKind, path string, connCount int) int {
	b.Helper()
	return measureWebSocketConnectionGoroutines(b, kind, path, connCount)
}

func measureIdleHTTPConnectionGoroutines(tb testing.TB, kind wsdemo.ServerKind, connCount int) int {
	tb.Helper()
	addr := nextLoopbackAddrForBench(tb)
	server := wsdemo.NewServer(kind, addr, filepathForBench(tb))
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	waitForHTTPServerBench(tb, addr, errCh)

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	conns := make([]net.Conn, 0, connCount)
	for i := 0; i < connCount; i++ {
		conn, err := net.Dial("tcp", "127.0.0.1"+addr)
		if err != nil {
			tb.Fatalf("Dial() error = %v", err)
		}
		conns = append(conns, conn)
	}
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	for _, conn := range conns {
		_ = conn.Close()
	}
	shutdownBenchServer(tb, server, errCh)
	return after - before
}

func measureWebSocketConnectionGoroutines(tb testing.TB, kind wsdemo.ServerKind, path string, connCount int) int {
	tb.Helper()
	addr := nextLoopbackAddrForBench(tb)
	server := wsdemo.NewServer(kind, addr, filepathForBench(tb))
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	waitForHTTPServerBench(tb, addr, errCh)

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	wsURL := "ws://127.0.0.1" + addr + path
	conns := make([]*websocket.Conn, 0, connCount)
	for i := 0; i < connCount; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			tb.Fatalf("Dial(%s) error = %v", path, err)
		}
		conns = append(conns, conn)
	}
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	for _, conn := range conns {
		_ = conn.Close()
	}
	shutdownBenchServer(tb, server, errCh)
	return after - before
}

func filepathForBench(tb testing.TB) string {
	tb.Helper()
	return "../go.mod"
}

func shutdownBenchServer(tb testing.TB, server *wsdemo.Server, errCh <-chan error) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		tb.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			tb.Fatalf("ListenAndServe() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		tb.Fatal("timed out waiting for server shutdown")
	}
}

func waitForHTTPServerBench(tb testing.TB, addr string, errCh <-chan error) {
	tb.Helper()
	url := fmt.Sprintf("http://127.0.0.1%s/", addr)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		select {
		case serveErr := <-errCh:
			tb.Fatalf("server failed before becoming ready: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			tb.Fatalf("server at %s did not become ready: %v", addr, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func nextLoopbackAddrForBench(tb testing.TB) string {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)
}
