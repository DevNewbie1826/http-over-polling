package hop

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/DevNewbie1826/http-over-polling/transport"
)

type legacyHijacker interface {
	Hijack() (net.Conn, *bufio.ReadWriter, error)
	SetReadHandler(func(net.Conn, *bufio.ReadWriter) error)
}

type hijackTestConn struct {
	*testConn
	hijackedConn net.Conn
	rw           *bufio.ReadWriter
	handler      func(net.Conn, *bufio.ReadWriter) error
}

func newHijackTestConn(in []byte) *hijackTestConn {
	base := newTestConn(in)
	reader := bufio.NewReader(bytes.NewReader(nil))
	writer := bufio.NewWriter(ioDiscardWriter{})
	return &hijackTestConn{
		testConn:     base,
		hijackedConn: stubNetConn{},
		rw:           bufio.NewReadWriter(reader, writer),
	}
}

func (c *hijackTestConn) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return c.hijackedConn, c.rw, nil
}

func (c *hijackTestConn) SetReadHandler(h func(net.Conn, *bufio.ReadWriter) error) {
	c.handler = h
}

type ioDiscardWriter struct{}

func (ioDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

type stubNetConn struct{}

func (stubNetConn) Read([]byte) (int, error)         { return 0, nil }
func (stubNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (stubNetConn) Close() error                     { return nil }
func (stubNetConn) LocalAddr() net.Addr              { return testAddr("127.0.0.1:8080") }
func (stubNetConn) RemoteAddr() net.Addr             { return testAddr("127.0.0.1:12345") }
func (stubNetConn) SetDeadline(time.Time) error      { return nil }
func (stubNetConn) SetReadDeadline(time.Time) error  { return nil }
func (stubNetConn) SetWriteDeadline(time.Time) error { return nil }

var _ transport.Conn = (*hijackTestConn)(nil)

func TestResponseWriterSupportsLegacyHijackAndSetReadHandler(t *testing.T) {
	conn := newHijackTestConn([]byte("GET /ws HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	var callbackConn net.Conn
	var callbackRW *bufio.ReadWriter

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(legacyHijacker)
		if !ok {
			t.Fatalf("response writer does not implement legacy hijacker: %T", w)
		}
		connFromHijack, rw, err := hj.Hijack()
		if err != nil {
			t.Fatalf("Hijack() error = %v", err)
		}
		if connFromHijack == nil {
			t.Fatal("Hijack() returned nil conn")
		}
		if rw == nil {
			t.Fatal("Hijack() returned nil readwriter")
		}
		hj.SetReadHandler(func(c net.Conn, rw *bufio.ReadWriter) error {
			callbackConn = c
			callbackRW = rw
			return nil
		})
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if conn.handler == nil {
		t.Fatal("SetReadHandler did not register handler")
	}
	if err := conn.handler(conn.hijackedConn, conn.rw); err != nil {
		t.Fatalf("registered handler error = %v", err)
	}
	if callbackConn != conn.hijackedConn {
		t.Fatal("registered handler received unexpected conn")
	}
	if callbackRW != conn.rw {
		t.Fatal("registered handler received unexpected readwriter")
	}
}

func TestResponseWriterFlushBranches(t *testing.T) {
	t.Run("returns early when hijacked", func(t *testing.T) {
		buf := &flushBuffer{}
		w := &httpResponseWriter{hijacked: true, writer: bufio.NewWriterSize(buf, 64)}
		w.Flush()
		if got := buf.String(); got != "" {
			t.Fatalf("buffer = %q, want empty", got)
		}
	})

	t.Run("returns early when writer is nil", func(t *testing.T) {
		w := &httpResponseWriter{}
		w.Flush()
	})

	t.Run("flushes pending body through chunked path", func(t *testing.T) {
		buf := &flushBuffer{}
		w := &httpResponseWriter{
			protoMajor: 1,
			protoMinor: 1,
			request:    &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)},
			writer:     bufio.NewWriterSize(buf, 64),
		}
		if _, err := w.Write([]byte("A")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if _, err := w.Write([]byte("B")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		w.Flush()
		if out := buf.String(); !bytes.Contains([]byte(out), []byte("Transfer-Encoding: chunked\r\n")) {
			t.Fatalf("missing chunked response after Flush(): %q", out)
		}
	})
}
