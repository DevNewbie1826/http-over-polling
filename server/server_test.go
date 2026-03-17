package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DevNewbie1826/http-over-polling/engine"
)

func TestServer_ServeAndShutdown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	eng := engine.NewEngine(mux)

	// Use Options
	srv := NewServer(eng,
		WithReadTimeout(1*time.Second),
		WithWriteTimeout(1*time.Second),
		WithKeepAliveTimeout(1*time.Second),
	)

	// Start server in goroutine
	go func() {
		if err := srv.Serve(":19999"); err != nil {
			// Serve might return error on shutdown, which is expected
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Make a request
	conn, err := net.Dial("tcp", "127.0.0.1:19999")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	response := string(buf[:n])
	if response[:15] != "HTTP/1.1 200 OK" {
		t.Errorf("Unexpected response: %s", response)
	}
	conn.Close()

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestNewServerDefaultsPollerNum(t *testing.T) {
	mux := http.NewServeMux()
	eng := engine.NewEngine(mux)
	srv := NewServer(eng)

	want := runtime.GOMAXPROCS(0) / 4
	if want < 1 {
		want = 1
	}

	if srv.pollerNum != want {
		t.Fatalf("default pollerNum = %d, want %d", srv.pollerNum, want)
	}
}

func TestWithPollerNum(t *testing.T) {
	mux := http.NewServeMux()
	eng := engine.NewEngine(mux)
	srv := NewServer(eng, WithPollerNum(7))

	if srv.pollerNum != 7 {
		t.Fatalf("pollerNum = %d, want 7", srv.pollerNum)
	}
}

func TestWithBufferSize(t *testing.T) {
	mux := http.NewServeMux()
	eng := engine.NewEngine(mux)
	srv := NewServer(eng, WithBufferSize(512))

	if srv.bufferSize != 512 {
		t.Fatalf("bufferSize = %d, want 512", srv.bufferSize)
	}
}

func TestServerServeUsesConfiguredPollerNum(t *testing.T) {
	data, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	src := string(data)
	if !strings.Contains(src, "PollerNum: s.pollerNum") {
		t.Fatal("Serve() must configure netpoll with Server.pollerNum")
	}
	if !strings.Contains(src, "BufferSize: s.bufferSize") {
		t.Fatal("Serve() must configure netpoll with Server.bufferSize")
	}
}
