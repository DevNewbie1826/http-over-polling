package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHopNoLongerForcesPollerNumToGOMAXPROCS(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	src := string(data)
	if strings.Contains(src, "PollerNum: runtime.GOMAXPROCS(0)") {
		t.Fatal("hop() must not force PollerNum to runtime.GOMAXPROCS(0)")
	}
}

func TestRootHandlerUsesHopBranding(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	rootHandler(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "Welcome to Hop WebSocket Examples!") {
		t.Fatalf("root body = %q, want hop branding", body)
	}
}

func TestFileHandlerServesCurrentExampleSource(t *testing.T) {
	req := httptest.NewRequest("GET", "/file", nil)
	rr := httptest.NewRecorder()

	fileHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "package main") {
		t.Fatalf("file body missing Go source header: %q", rr.Body.String())
	}
}

func TestCloseAllClosesHTTPConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(io.Discard, conn)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	closeAll([]io.Closer{conn})

	if _, err := conn.Write([]byte("ping")); err == nil {
		t.Fatal("expected closed connection write to fail")
	}

	<-done
}

func TestGoroutineBenchmarkConnCountFromRlimit(t *testing.T) {
	got := goroutineBenchmarkConnCountFromRlimit(1024)
	if got != 480 {
		t.Fatalf("goroutineBenchmarkConnCountFromRlimit(1024) = %d, want 480", got)
	}
}

func TestGoroutineBenchmarkConnCountFromRlimitUnlimitedFallsBackToProbe(t *testing.T) {
	got := goroutineBenchmarkConnCountFromRlimit(1 << 62)
	if got != 0 {
		t.Fatalf("goroutineBenchmarkConnCountFromRlimit(unlimited) = %d, want 0", got)
	}
}

func TestGoroutineBenchmarkConnCountFromPortRange(t *testing.T) {
	got := goroutineBenchmarkConnCountFromPortRange(49152, 65535)
	if got != 15360 {
		t.Fatalf("goroutineBenchmarkConnCountFromPortRange(...) = %d, want 15360", got)
	}
}
