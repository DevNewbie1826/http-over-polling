package hop

import (
	"net/http"
	"testing"

	"github.com/DevNewbie1826/http-over-polling/transport"
)

func TestEventsInitializesHttpConnAndServesRequest(t *testing.T) {
	conn := newTestConn([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	events := Events(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	events.OnOpen(conn)
	ctx := conn.Context()
	if _, ok := ctx.(*HttpConn); !ok {
		t.Fatalf("Context() type = %T, want *HttpConn", ctx)
	}
	if err := events.OnData(conn); err != nil {
		t.Fatalf("OnData() error = %v", err)
	}
	if got := conn.out.String(); got == "" {
		t.Fatal("response output is empty")
	}
}

func TestHopListenAndServeReturnsTransportError(t *testing.T) {
	err := ListenAndServe("bad-addr", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), transport.WithReadTimeout(0))
	if err == nil {
		t.Fatal("ListenAndServe() error = nil, want non-nil")
	}
}
