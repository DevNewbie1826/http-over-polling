package hop

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"unsafe"

	httpparser "github.com/DevNewbie1826/http-over-polling/internal/parser"
	"github.com/DevNewbie1826/http-over-polling/transport"
)

type testReadLease struct{ view []byte }

func (l *testReadLease) Bytes() []byte { return l.view }
func (l *testReadLease) Retain() transport.ReadLease {
	return l
}
func (l *testReadLease) Release() {}

type countingReadLease struct {
	view         []byte
	releaseCount *int
}

func (l *countingReadLease) Bytes() []byte { return l.view }
func (l *countingReadLease) Retain() transport.ReadLease {
	return &countingReadLease{view: l.view, releaseCount: l.releaseCount}
}
func (l *countingReadLease) Release() {
	if l.releaseCount != nil {
		*l.releaseCount += 1
	}
}

type testAddr string

func (a testAddr) Network() string { return "tcp" }
func (a testAddr) String() string  { return string(a) }

type testConn struct {
	in               []byte
	out              bytes.Buffer
	ctx              any
	local            net.Addr
	remote           net.Addr
	lease            testReadLease
	closed           bool
	readLeaseCalls   int
	writeLeaseCalls  int
	writeHeaderCalls int
	completeCalls    int
}

type countingConn struct {
	*testConn
	releases int
}

func newTestConn(in []byte) *testConn {
	return &testConn{
		in:     append([]byte(nil), in...),
		local:  testAddr("127.0.0.1:8080"),
		remote: testAddr("127.0.0.1:12345"),
	}
}

func serveUntilInputDrainedForTest(hc *HttpConn, conn *testConn, maxCalls int) (int, error) {
	calls := 0
	for len(conn.in) > 0 {
		if calls == maxCalls {
			return calls, nil
		}
		before := len(conn.in)
		if err := hc.Serve(); err != nil {
			return calls, err
		}
		calls++
		if len(conn.in) >= before {
			return calls, nil
		}
	}
	return calls, nil
}

func newCountingConn(in []byte) *countingConn {
	return &countingConn{testConn: newTestConn(in)}
}

func (c *testConn) Write(b []byte) (int, error) { return c.out.Write(b) }
func (c *testConn) WriteLease(lease transport.WriteLease) (int, error) {
	c.writeLeaseCalls++
	defer lease.Release()
	return c.out.Write(lease.Bytes())
}
func (c *testConn) WriteHeaderAndLease(header []byte, lease transport.WriteLease) (int, error) {
	c.writeHeaderCalls++
	n, err := c.out.Write(header)
	if err != nil {
		lease.Release()
		return n, err
	}
	m, err := c.WriteLease(lease)
	return n + m, err
}
func (c *testConn) Writev(chunks ...[]byte) (int, error) {
	total := 0
	for _, chunk := range chunks {
		n, err := c.Write(chunk)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
func (c *testConn) Peek(b []byte) []byte {
	if len(c.in) == 0 {
		return nil
	}
	return c.in
}
func (c *testConn) AcquireReadView() []byte {
	c.readLeaseCalls++
	return c.in
}
func (c *testConn) RetainReadView()                       { c.readLeaseCalls++ }
func (c *testConn) ReleaseReadView()                      {}
func (c *testConn) WriteLeaseBytes(p []byte) (int, error) { return c.out.Write(p) }
func (c *testConn) AcquireRead() transport.ReadLease {
	c.readLeaseCalls++
	c.lease.view = c.in
	return &c.lease
}
func (c *countingConn) AcquireRead() transport.ReadLease {
	c.readLeaseCalls++
	return &countingReadLease{view: c.in, releaseCount: &c.releases}
}
func (c *testConn) Discard(n int) (int, error) {
	if n > len(c.in) {
		n = len(c.in)
	}
	c.in = c.in[n:]
	return n, nil
}
func (c *testConn) Close() error         { c.closed = true; return nil }
func (c *testConn) PauseRead()           {}
func (c *testConn) ResumeRead()          {}
func (c *testConn) CompleteRequest()     { c.completeCalls++ }
func (c *testConn) Context() any         { return c.ctx }
func (c *testConn) SetContext(v any)     { c.ctx = v }
func (c *testConn) LocalAddr() net.Addr  { return c.local }
func (c *testConn) RemoteAddr() net.Addr { return c.remote }

var _ transport.Conn = (*testConn)(nil)

func TestBuildsRequest(t *testing.T) {
	conn := newTestConn([]byte("GET /hello?name=hop HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\nX-Test: yes\r\n\r\n"))
	var captured *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if captured == nil {
		t.Fatal("captured request is nil")
	}
	if captured.Method != http.MethodGet {
		t.Fatalf("method = %q, want %q", captured.Method, http.MethodGet)
	}
	if captured.RequestURI != "/hello?name=hop" {
		t.Fatalf("requestURI = %q", captured.RequestURI)
	}
	if captured.Host != "example.com" {
		t.Fatalf("host = %q", captured.Host)
	}
	if captured.Header.Get("X-Test") != "yes" {
		t.Fatalf("X-Test = %q", captured.Header.Get("X-Test"))
	}
	if !captured.Close {
		t.Fatal("request.Close = false, want true")
	}
}

func TestBuildsRequestWithCaseInsensitiveSpecialHeaders(t *testing.T) {
	conn := newTestConn([]byte("GET /hello HTTP/1.1\r\nhost: example.com\r\nconnection: close\r\n\r\n"))
	var captured *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if captured == nil {
		t.Fatal("captured request is nil")
	}
	if captured.Host != "example.com" {
		t.Fatalf("host = %q, want %q", captured.Host, "example.com")
	}
	if !captured.Close {
		t.Fatal("request.Close = false, want true")
	}
}

func TestBuildsRequestWithMultipleConnectionHeaders(t *testing.T) {
	conn := newTestConn([]byte("GET /hello HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\nConnection: close\r\n\r\n"))
	var captured *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if captured == nil {
		t.Fatal("captured request is nil")
	}
	if !captured.Close {
		t.Fatal("request.Close = false, want true")
	}
}

func TestBuildsRequestWithConnectionAndUpgradeHeaders(t *testing.T) {
	conn := newTestConn([]byte("GET /ws HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"))
	var captured *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if captured == nil {
		t.Fatal("captured request is nil")
	}
	if got := captured.Header.Get("Connection"); got != "Upgrade" {
		t.Fatalf("Connection header = %q, want %q", got, "Upgrade")
	}
	if got := captured.Header.Get("Upgrade"); got != "websocket" {
		t.Fatalf("Upgrade header = %q, want %q", got, "websocket")
	}
}

func TestBuildsChunkedRequest(t *testing.T) {
	conn := newTestConn([]byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n"))
	var body string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		body = string(payload)
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if body != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}
}

func TestBuildsMultiChunkedRequest(t *testing.T) {
	conn := newTestConn([]byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n6\r\nhello \r\n5\r\nworld\r\n0\r\n\r\n"))
	var body string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		body = string(payload)
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if body != "hello world" {
		t.Fatalf("body = %q, want %q", body, "hello world")
	}
}

func TestBuildsRequestZeroCopyMetadataSingleChunk(t *testing.T) {
	input := []byte("GET /hello?name=hop HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\nX-Test: yes\r\n\r\n")
	conn := newTestConn(input)
	uriIndex := bytes.Index(conn.in, []byte("/hello?name=hop"))
	hostIndex := bytes.Index(conn.in, []byte("example.com"))
	headerIndex := bytes.Index(conn.in, []byte("yes"))
	if uriIndex < 0 || hostIndex < 0 || headerIndex < 0 {
		t.Fatal("failed to find expected substrings in input")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raceSafeCopiesEnabled() {
			if unsafe.StringData(r.RequestURI) == &conn.in[uriIndex] {
				t.Fatal("RequestURI unexpectedly aliases input bytes under race build")
			}
			if unsafe.StringData(r.Host) == &conn.in[hostIndex] {
				t.Fatal("Host unexpectedly aliases input bytes under race build")
			}
			if unsafe.StringData(r.Header.Get("X-Test")) == &conn.in[headerIndex] {
				t.Fatal("header value unexpectedly aliases input bytes under race build")
			}
		} else {
			if unsafe.StringData(r.RequestURI) != &conn.in[uriIndex] {
				t.Fatal("RequestURI does not alias input bytes")
			}
			if unsafe.StringData(r.Host) != &conn.in[hostIndex] {
				t.Fatal("Host does not alias input bytes")
			}
			if unsafe.StringData(r.Header.Get("X-Test")) != &conn.in[headerIndex] {
				t.Fatal("header value does not alias input bytes")
			}
		}
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestBuildsRequestZeroCopyBodySingleChunk(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	conn := newTestConn(input)
	bodyIndex := bytes.Index(conn.in, []byte("hello"))
	if bodyIndex < 0 {
		t.Fatal("failed to find body in input")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := r.Body.(*readLeaseBody)
		if !ok {
			t.Fatalf("Body type = %T, want *readLeaseBody", r.Body)
		}
		if raceSafeCopiesEnabled() {
			if &body.view[0] == &conn.in[bodyIndex] {
				t.Fatal("body view unexpectedly aliases input bytes under race build")
			}
		} else if &body.view[0] != &conn.in[bodyIndex] {
			t.Fatal("body view does not alias input bytes")
		}
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestBuildsRequestZeroCopySplitRequestURI(t *testing.T) {
	conn := newTestConn(nil)
	var captured *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	hc.parser.Method = httpparser.GET
	hc.parser.Major = 1
	hc.parser.Minor = 1
	hc.setting.MessageBegin(hc.parser, 0)
	hc.setting.URL(hc.parser, []byte("/hel"), 0)
	hc.setting.URL(hc.parser, []byte("lo?name=hop"), 0)
	hc.setting.HeaderField(hc.parser, []byte("Host"), 0)
	hc.setting.HeaderValue(hc.parser, []byte("example.com"), 0)
	hc.setting.HeadersComplete(hc.parser, 0)
	hc.setting.MessageComplete(hc.parser, 0)
	if captured == nil {
		t.Fatal("captured request is nil")
	}
	if captured.RequestURI != "/hello?name=hop" {
		t.Fatalf("requestURI = %q, want %q", captured.RequestURI, "/hello?name=hop")
	}
	if len(hc.requestURIBuf) == 0 {
		t.Fatal("requestURIBuf is empty")
	}
	if unsafe.StringData(captured.RequestURI) == &hc.requestURIBuf[0] {
		t.Fatal("split RequestURI unexpectedly aliases requestURIBuf")
	}
}

func TestSplitRequestURIStaysStableAcrossScratchReuse(t *testing.T) {
	conn := newTestConn(nil)
	var uris []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.RequestURI)
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	hc.parser.Method = httpparser.GET
	hc.parser.Major = 1
	hc.parser.Minor = 1

	hc.setting.MessageBegin(hc.parser, 0)
	hc.setting.URL(hc.parser, []byte("/fir"), 0)
	hc.setting.URL(hc.parser, []byte("st?name=hop"), 0)
	hc.setting.HeaderField(hc.parser, []byte("Host"), 0)
	hc.setting.HeaderValue(hc.parser, []byte("example.com"), 0)
	hc.setting.HeadersComplete(hc.parser, 0)
	hc.setting.MessageComplete(hc.parser, 0)

	hc.setting.MessageBegin(hc.parser, 0)
	hc.setting.URL(hc.parser, []byte("/sec"), 0)
	hc.setting.URL(hc.parser, []byte("ond?name=netpoll"), 0)
	hc.setting.HeaderField(hc.parser, []byte("Host"), 0)
	hc.setting.HeaderValue(hc.parser, []byte("example.com"), 0)
	hc.setting.HeadersComplete(hc.parser, 0)
	hc.setting.MessageComplete(hc.parser, 0)

	if len(uris) != 2 {
		t.Fatalf("captured uri count = %d, want 2", len(uris))
	}
	if uris[0] != "/first?name=hop" {
		t.Fatalf("first uri = %q, want %q", uris[0], "/first?name=hop")
	}
	if uris[1] != "/second?name=netpoll" {
		t.Fatalf("second uri = %q, want %q", uris[1], "/second?name=netpoll")
	}
}

func TestBuildsRequestWhenContentLengthIsFirstHeader(t *testing.T) {
	conn := newTestConn([]byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\nHost: example.com\r\n\r\nhello"))
	var body string
	var captured *http.Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		body = string(payload)
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if body != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}
	if captured == nil {
		t.Fatal("captured request is nil")
	}
	if captured.ContentLength != 5 {
		t.Fatalf("ContentLength = %d, want 5", captured.ContentLength)
	}
	if got := captured.Header.Get("Content-Length"); got != "5" {
		t.Fatalf("Header.Get(Content-Length) = %q, want %q", got, "5")
	}
}

func TestFirstRequestWindowIncludesWholeChunkedRequest(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")
	if got, want := firstRequestWindow(input), len(input); got != want {
		t.Fatalf("firstRequestWindow() = %d, want %d", got, want)
	}
}

func TestFirstRequestWindowIncludesWholeContentLengthRequest(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	if got, want := firstRequestWindow(input), len(input); got != want {
		t.Fatalf("firstRequestWindow() = %d, want %d", got, want)
	}
}

func TestFirstRequestWindowStopsAtFirstContentLengthRequestInPipeline(t *testing.T) {
	first := []byte("POST /one HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	second := []byte("POST /two HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nworld")
	input := append(append([]byte(nil), first...), second...)
	if got, want := firstRequestWindow(input), len(first); got != want {
		t.Fatalf("firstRequestWindow() = %d, want %d", got, want)
	}
}

func TestServeDrainsBufferedPipelineInSingleDispatch(t *testing.T) {
	conn := newTestConn([]byte("GET /one HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\n\r\nGET /two HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "/one") {
		t.Fatalf("first response missing body: %q", out)
	}
	if !strings.Contains(out, "/two") {
		t.Fatalf("second response missing body: %q", out)
	}
	if conn.completeCalls != 2 {
		t.Fatalf("CompleteRequest calls = %d, want 2", conn.completeCalls)
	}
	if len(conn.in) != 0 {
		t.Fatalf("remaining input = %q, want empty after single dispatch drain", string(conn.in))
	}
}

func TestServeExperimentDrainsBufferedPipelineWhenReentered(t *testing.T) {
	conn := newTestConn([]byte("GET /one HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\n\r\nGET /two HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	})

	hc := NewHttpConn(conn, handler)
	calls, err := serveUntilInputDrainedForTest(hc, conn, 4)
	if err != nil {
		t.Fatalf("serveUntilInputDrainedForTest() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("Serve() calls = %d, want 1 after single-dispatch drain", calls)
	}
	if conn.completeCalls != 2 {
		t.Fatalf("CompleteRequest calls = %d, want 2", conn.completeCalls)
	}
	if len(conn.in) != 0 {
		t.Fatalf("remaining input = %q, want empty after repeated Serve()", string(conn.in))
	}
	out := conn.out.String()
	if !strings.Contains(out, "/one") || !strings.Contains(out, "/two") {
		t.Fatalf("responses = %q, want both pipeline responses", out)
	}
}

func TestParserOwnedFramingExperimentCurrentlyConsumesFullBuffer(t *testing.T) {
	input := []byte("GET /one HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\n\r\nGET /two HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
	conn := newTestConn(input)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	})

	hc := NewHttpConn(conn, handler)
	hc.handleErr = nil
	hc.requestDone = false
	parsedBytes, err := hc.parser.Execute(hc.setting, conn.in)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if hc.handleErr != nil {
		t.Fatalf("handleErr = %v", hc.handleErr)
	}
	if !hc.requestDone {
		t.Fatal("requestDone = false, want true after first request parse")
	}
	if parsedBytes <= 0 {
		t.Fatalf("parsedBytes = %d, want > 0", parsedBytes)
	}
	if parsedBytes != len(input) {
		t.Fatalf("parsedBytes = %d, want %d when parser consumes full buffer", parsedBytes, len(input))
	}
	if _, err := conn.Discard(parsedBytes); err != nil {
		t.Fatalf("Discard() error = %v", err)
	}
	if !strings.Contains(conn.out.String(), "/one") {
		t.Fatalf("first response missing body: %q", conn.out.String())
	}
	if strings.Contains(conn.out.String(), "/two") {
		t.Fatalf("second response should not be written in one parser execution: %q", conn.out.String())
	}
	if len(conn.in) != 0 {
		t.Fatalf("remaining input = %q, want parser-owned framing experiment to drain full buffer", string(conn.in))
	}
}

func TestServeUsesAcquireReadAndLeaseWritePath(t *testing.T) {
	conn := newTestConn([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if conn.readLeaseCalls == 0 {
		t.Fatal("AcquireRead was not used")
	}
	if conn.writeLeaseCalls == 0 {
		t.Fatal("WriteLease was not used")
	}
	if conn.writeHeaderCalls == 0 {
		t.Fatal("WriteHeaderAndLease was not used")
	}
	if conn.completeCalls != 1 {
		t.Fatalf("CompleteRequest calls = %d, want 1", conn.completeCalls)
	}
}

func TestServeClosesTransportConnWhenRequestWantsClose(t *testing.T) {
	conn := newTestConn([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if !conn.closed {
		t.Fatal("transport conn was not closed for Connection: close request")
	}
}

func TestZeroCopyBodyRetainsLeaseUntilBodyClose(t *testing.T) {
	conn := newCountingConn([]byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello"))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("body = %q, want %q", string(payload), "hello")
		}
		_, _ = w.Write([]byte("ok"))
	})

	hc := NewHttpConn(conn, handler)
	if err := hc.Serve(); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	wantReleases := 2
	if raceSafeCopiesEnabled() {
		wantReleases = 1
	}
	if conn.releases != wantReleases {
		t.Fatalf("read lease releases = %d, want %d", conn.releases, wantReleases)
	}
}
