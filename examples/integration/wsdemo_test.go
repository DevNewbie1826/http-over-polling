package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wsdemo "github.com/DevNewbie1826/http-over-polling/examples/internal/wsapp"
	"github.com/gorilla/websocket"
)

func TestWebSocketExampleHTTPRoutes(t *testing.T) {
	t.Parallel()

	for _, kind := range []wsdemo.ServerKind{wsdemo.ServerKindStd, wsdemo.ServerKindHop} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			baseURL, shutdown := startWSDemoServer(t, kind)
			defer shutdown()

			resp, err := http.Get(baseURL + "/")
			if err != nil {
				t.Fatalf("GET / error = %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatalf("ReadAll(/) error = %v", err)
			}
			if !strings.Contains(string(body), "/ws-gobwas-low") || !strings.Contains(string(body), "/ws-gorilla-event") {
				t.Fatalf("root body = %q, want hon example route listing", string(body))
			}

			resp, err = http.Get(baseURL + "/file")
			if err != nil {
				t.Fatalf("GET /file error = %v", err)
			}
			body, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatalf("ReadAll(/file) error = %v", err)
			}
			if !strings.Contains(string(body), "package main") {
				t.Fatalf("file body = %q, want Go source contents", string(body))
			}
		})
	}
}

func TestWebSocketExampleSSE(t *testing.T) {
	t.Parallel()

	for _, kind := range []wsdemo.ServerKind{wsdemo.ServerKindStd, wsdemo.ServerKindHop} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			baseURL, shutdown := startWSDemoServer(t, kind)
			defer shutdown()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/sse", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET /sse error = %v", err)
			}

			reader := bufio.NewReader(resp.Body)
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("ReadString() error = %v", err)
			}
			if !strings.HasPrefix(line, "data: Server time is ") {
				t.Fatalf("sse line = %q, want hon example data prefix", line)
			}
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatalf("second ReadString() error = %v", err)
			}
			line, err = reader.ReadString('\n')
			if err != nil {
				t.Fatalf("third ReadString() error = %v", err)
			}
			if !strings.HasPrefix(line, "data: Server time is ") {
				t.Fatalf("second event line = %q, want repeated SSE data prefix", line)
			}
			cancel()
			_ = resp.Body.Close()
		})
	}
}

func TestWebSocketExampleEchoRoutes(t *testing.T) {
	testCases := []struct {
		name string
		kind wsdemo.ServerKind
		path string
	}{
		{name: "std-gorilla-std", kind: wsdemo.ServerKindStd, path: "/ws-gorilla-std"},
		{name: "hop-gobwas-low", kind: wsdemo.ServerKindHop, path: "/ws-gobwas-low"},
		{name: "hop-gobwas-high", kind: wsdemo.ServerKindHop, path: "/ws-gobwas-high"},
		{name: "hop-gorilla-std", kind: wsdemo.ServerKindHop, path: "/ws-gorilla-std"},
		{name: "hop-gorilla-event", kind: wsdemo.ServerKindHop, path: "/ws-gorilla-event"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			baseURL, shutdown := startWSDemoServer(t, tc.kind)
			defer shutdown()

			wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + tc.path
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("Dial(%s) error = %v", tc.path, err)
			}
			defer conn.Close()

			want := []byte("hello websocket")
			if err := conn.WriteMessage(websocket.TextMessage, want); err != nil {
				t.Fatalf("WriteMessage() error = %v", err)
			}
			messageType, got, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("ReadMessage() error = %v", err)
			}
			if messageType != websocket.TextMessage {
				t.Fatalf("messageType = %d, want %d", messageType, websocket.TextMessage)
			}
			if string(got) != string(want) {
				t.Fatalf("payload = %q, want %q", string(got), string(want))
			}
		})
	}
}

func startWSDemoServer(t *testing.T, kind wsdemo.ServerKind) (string, func()) {
	t.Helper()
	addr := nextLoopbackAddr(t)
	filePath := filepath.Join("..", "cmd", "ws_example", "main.go")
	server := wsdemo.NewServer(kind, addr, filePath)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	waitForHTTPServer(t, addr, errCh)
	return fmt.Sprintf("http://127.0.0.1%s", addr), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		select {
		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				t.Fatalf("ListenAndServe() error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for server shutdown")
		}
	}
}

func waitForHTTPServer(t *testing.T, addr string, errCh <-chan error) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1%s/", addr)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return
		}
		select {
		case serveErr := <-errCh:
			t.Fatalf("server failed before becoming ready: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server at %s did not become ready: %v", addr, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
