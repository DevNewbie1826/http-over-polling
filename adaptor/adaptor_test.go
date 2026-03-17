package adaptor

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/DevNewbie1826/http-over-polling/appcontext"
	"github.com/cloudwego/netpoll"
)

// MockConn implements netpoll.Connection partially for testing
type MockConn struct {
	netpoll.Connection
	buf *bytes.Buffer
}

func (m *MockConn) Reader() netpoll.Reader { return nil }
func (m *MockConn) Writer() netpoll.Writer { return netpoll.NewWriter(m.buf) }
func (m *MockConn) IsActive() bool         { return true }
func (m *MockConn) Close() error           { return nil }
func (m *MockConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
}
func (m *MockConn) Read(p []byte) (int, error) { return 0, io.EOF }

func newTestRequestContext(t *testing.T, raw string) *appcontext.RequestContext {
	if t != nil {
		t.Helper()
	}

	reader := bufio.NewReader(bytes.NewBufferString(raw))
	writer := bufio.NewWriter(io.Discard)
	return appcontext.NewRequestContext(&MockConn{buf: new(bytes.Buffer)}, context.Background(), reader, writer)
}

func TestGetRequest_PreservesMethodURLHostRemoteAddrAndContext(t *testing.T) {
	parent := context.WithValue(context.Background(), testContextKey{}, "marker")
	rawBody := "hello"
	raw := "POST /submit?debug=1 HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Content-Length: " + strconv.Itoa(len(rawBody)) + "\r\n" +
		"X-Test: value\r\n\r\n" +
		rawBody
	ctx := appcontext.NewRequestContext(&MockConn{buf: new(bytes.Buffer)}, parent, bufio.NewReader(bytes.NewBufferString(raw)), bufio.NewWriter(io.Discard))
	defer ctx.Release()

	req, err := GetRequest(ctx)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	if req.Method != http.MethodPost {
		t.Fatalf("Method = %q, want %q", req.Method, http.MethodPost)
	}
	if req.URL.Path != "/submit" {
		t.Fatalf("URL.Path = %q, want %q", req.URL.Path, "/submit")
	}
	if req.URL.RawQuery != "debug=1" {
		t.Fatalf("URL.RawQuery = %q, want %q", req.URL.RawQuery, "debug=1")
	}
	if req.RequestURI != "/submit?debug=1" {
		t.Fatalf("RequestURI = %q, want %q", req.RequestURI, "/submit?debug=1")
	}
	if req.Host != "example.com" {
		t.Fatalf("Host = %q, want %q", req.Host, "example.com")
	}
	if req.RemoteAddr != "127.0.0.1:1234" {
		t.Fatalf("RemoteAddr = %q, want %q", req.RemoteAddr, "127.0.0.1:1234")
	}
	if got := req.Context().Value(testContextKey{}); got != "marker" {
		t.Fatalf("Context().Value(testContextKey{}) = %#v, want %q", got, "marker")
	}
	if req.Context() != parent {
		t.Fatal("request context was not inherited from RequestContext")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}
	if string(body) != rawBody {
		t.Fatalf("body = %q, want %q", string(body), rawBody)
	}
	if req.ContentLength != int64(len(rawBody)) {
		t.Fatalf("ContentLength = %d, want %d", req.ContentLength, len(rawBody))
	}
	if got := req.Header.Get("X-Test"); got != "value" {
		t.Fatalf("Header.Get(X-Test) = %q, want %q", got, "value")
	}
	if got := req.Header.Get("Host"); got != "" {
		t.Fatalf("Header.Get(Host) = %q, want empty", got)
	}
}

type testContextKey struct{}

func TestGetRequest_DecodesChunkedBody(t *testing.T) {
	raw := "POST /chunked HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Transfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
	ctx := newTestRequestContext(t, raw)
	defer ctx.Release()

	req, err := GetRequest(ctx)
	if err != nil {
		t.Fatalf("GetRequest() error = %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q, want %q", string(body), "hello world")
	}
	if req.ContentLength != -1 {
		t.Fatalf("ContentLength = %d, want -1", req.ContentLength)
	}
	if len(req.TransferEncoding) != 1 || req.TransferEncoding[0] != "chunked" {
		t.Fatalf("TransferEncoding = %#v, want []string{\"chunked\"}", req.TransferEncoding)
	}
}

func TestGetRequest_BackgroundContextAllocationBudget(t *testing.T) {
	raw := "POST /joyent/http-parser HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"DNT: 1\r\n" +
		"Accept-Encoding: gzip, deflate, sdch\r\n" +
		"Accept-Language: ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4\r\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.65 Safari/537.36\r\n" +
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
		"Referer: https://github.com/joyent/http-parser\r\n" +
		"Connection: keep-alive\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Cache-Control: max-age=0\r\n\r\n" +
		"b\r\nhello world\r\n0\r\n\r\n"

	allocs := testing.AllocsPerRun(1000, func() {
		ctx := newTestRequestContext(nil, raw)
		ctx.SetRemoteAddrString("127.0.0.1:8080")
		req, err := GetRequest(ctx)
		if err != nil {
			t.Fatalf("GetRequest() error = %v", err)
		}
		if req.Context() != context.Background() {
			t.Fatalf("request context = %#v, want context.Background()", req.Context())
		}
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			t.Fatalf("io.Copy(req.Body) error = %v", err)
		}
		_ = req.Body.Close()
		ctx.Release()
	})

	if allocs > 40 {
		t.Fatalf("allocs = %v, want <= 40", allocs)
	}
}

func BenchmarkGetRequestHttparserBenchmarkFixture(b *testing.B) {
	raw := "POST /joyent/http-parser HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"DNT: 1\r\n" +
		"Accept-Encoding: gzip, deflate, sdch\r\n" +
		"Accept-Language: ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4\r\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.65 Safari/537.36\r\n" +
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
		"Referer: https://github.com/joyent/http-parser\r\n" +
		"Connection: keep-alive\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Cache-Control: max-age=0\r\n\r\n" +
		"b\r\nhello world\r\n0\r\n\r\n"
	benchmarkGetRequest(b, raw, true)
}

func BenchmarkGetRequestHttparserBenchmarkFixtureNoBodyDrain(b *testing.B) {
	raw := "POST /joyent/http-parser HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"DNT: 1\r\n" +
		"Accept-Encoding: gzip, deflate, sdch\r\n" +
		"Accept-Language: ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4\r\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.65 Safari/537.36\r\n" +
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
		"Referer: https://github.com/joyent/http-parser\r\n" +
		"Connection: keep-alive\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Cache-Control: max-age=0\r\n\r\n" +
		"b\r\nhello world\r\n0\r\n\r\n"
	benchmarkGetRequest(b, raw, false)
}

func BenchmarkGetRequestHttparserBenchmarkFixtureBackgroundContext(b *testing.B) {
	raw := "POST /joyent/http-parser HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"DNT: 1\r\n" +
		"Accept-Encoding: gzip, deflate, sdch\r\n" +
		"Accept-Language: ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4\r\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.65 Safari/537.36\r\n" +
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
		"Referer: https://github.com/joyent/http-parser\r\n" +
		"Connection: keep-alive\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Cache-Control: max-age=0\r\n\r\n" +
		"b\r\nhello world\r\n0\r\n\r\n"

	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))

	for i := 0; i < b.N; i++ {
		ctx := newTestRequestContext(nil, raw)
		ctx.SetRemoteAddrString("127.0.0.1:8080")
		req, err := GetRequest(ctx)
		if err != nil {
			b.Fatalf("GetRequest() error = %v", err)
		}
		if req.Context() != context.Background() {
			b.Fatalf("request context = %#v, want context.Background()", req.Context())
		}
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			b.Fatalf("io.Copy(req.Body) error = %v", err)
		}
		_ = req.Body.Close()
		ctx.Release()
	}
}

func BenchmarkGetRequestHttparserBenchmarkFixtureInheritedContext(b *testing.B) {
	raw := "POST /joyent/http-parser HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"DNT: 1\r\n" +
		"Accept-Encoding: gzip, deflate, sdch\r\n" +
		"Accept-Language: ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4\r\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.65 Safari/537.36\r\n" +
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8\r\n" +
		"Referer: https://github.com/joyent/http-parser\r\n" +
		"Connection: keep-alive\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Cache-Control: max-age=0\r\n\r\n" +
		"b\r\nhello world\r\n0\r\n\r\n"

	parent := context.WithValue(context.Background(), testContextKey{}, "marker")

	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))

	for i := 0; i < b.N; i++ {
		ctx := appcontext.NewRequestContext(&MockConn{buf: new(bytes.Buffer)}, parent, bufio.NewReader(bytes.NewBufferString(raw)), bufio.NewWriter(io.Discard))
		ctx.SetRemoteAddrString("127.0.0.1:8080")
		req, err := GetRequest(ctx)
		if err != nil {
			b.Fatalf("GetRequest() error = %v", err)
		}
		if got := req.Context().Value(testContextKey{}); got != "marker" {
			b.Fatalf("Context().Value(testContextKey{}) = %#v, want %q", got, "marker")
		}
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			b.Fatalf("io.Copy(req.Body) error = %v", err)
		}
		_ = req.Body.Close()
		ctx.Release()
	}
}

func benchmarkGetRequest(b *testing.B, raw string, drainBody bool) {
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))

	for i := 0; i < b.N; i++ {
		ctx := newTestRequestContext(nil, raw)
		req, err := GetRequest(ctx)
		if err != nil {
			b.Fatalf("GetRequest() error = %v", err)
		}
		if drainBody {
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				b.Fatalf("io.Copy(req.Body) error = %v", err)
			}
		}
		_ = req.Body.Close()
		ctx.Release()
	}
}

func TestResponseWriter_Write(t *testing.T) {
	buf := new(bytes.Buffer)
	// Inject a bufio.Writer writing to our buffer
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)

	rw := NewResponseWriter(ctx, nil)

	// Test Header Setting
	rw.Header().Set("X-Custom", "Foo")
	rw.WriteHeader(http.StatusCreated)

	// Test Write
	n, err := rw.Write([]byte("Hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 5 {
		t.Errorf("Expected 5 bytes written, got %d", n)
	}

	rw.Flush() // Flush to buffer

	resp := buf.String()
	if !contains(resp, "HTTP/1.1 201 Created") {
		t.Errorf("Status code missing")
	}
	if !contains(resp, "X-Custom: Foo") {
		t.Errorf("Header missing")
	}
	if !contains(resp, "Hello") {
		t.Errorf("Body missing")
	}

	rw.Release()
}

func TestResponseWriter_Chunked(t *testing.T) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)

	rw := NewResponseWriter(ctx, nil)
	// Do NOT set Content-Length -> Triggers Chunked

	rw.Write([]byte("Chunk1"))
	rw.Write([]byte("Chunk2"))
	rw.EndResponse()

	resp := buf.String()
	if !contains(resp, "Transfer-Encoding: chunked") {
		t.Errorf("Transfer-Encoding header missing")
	}
	// "6\r\nChunk1\r\n"
	if !contains(resp, "6\r\nChunk1\r\n") {
		t.Errorf("Chunk1 format incorrect")
	}
	// End "0\r\n\r\n"
	if !contains(resp, "0\r\n\r\n") {
		t.Errorf("Chunk end missing")
	}
	rw.Release()
}

func TestResponseWriter_SingleWriteSetsContentLength(t *testing.T) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)

	rw := NewResponseWriter(ctx, nil)
	if _, err := rw.Write([]byte("Hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := rw.EndResponse(); err != nil {
		t.Fatalf("EndResponse failed: %v", err)
	}

	resp := buf.String()
	if !contains(resp, "Content-Length: 5") {
		t.Fatalf("expected Content-Length header, got response:\n%s", resp)
	}
	if contains(resp, "Transfer-Encoding: chunked") {
		t.Fatalf("did not expect chunked transfer encoding, got response:\n%s", resp)
	}
	if contains(resp, "0\r\n\r\n") {
		t.Fatalf("did not expect chunk terminator, got response:\n%s", resp)
	}

	rw.Release()
}

func TestResponseWriter_LowercaseDirectMapContentTypeParity(t *testing.T) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)

	rw := NewResponseWriter(ctx, nil)
	rw.Header()["content-type"] = []string{"application/custom"}
	if _, err := rw.Write([]byte("Hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := rw.EndResponse(); err != nil {
		t.Fatalf("EndResponse failed: %v", err)
	}
	rw.Release()

	std := httptest.NewRecorder()
	std.Header()["content-type"] = []string{"application/custom"}
	if _, err := std.Write([]byte("Hello")); err != nil {
		t.Fatalf("std.Write failed: %v", err)
	}

	gotResp := buf.String()
	gotLower := contains(gotResp, "content-type: application/custom")
	gotSniffed := contains(gotResp, "Content-Type: text/plain")

	wantBody := std.Body.String()
	if wantBody != "Hello" {
		t.Fatalf("std body = %q, want Hello", wantBody)
	}

	wantLower := std.Header()["content-type"]
	wantCanonical := std.Header()["Content-Type"]
	if gotLower != (len(wantLower) > 0) {
		t.Fatalf("lowercase content-type presence = %v, want %v; response:\n%s", gotLower, len(wantLower) > 0, gotResp)
	}
	if gotSniffed != (len(wantCanonical) > 0) {
		t.Fatalf("canonical sniffed content-type presence = %v, want %v; response:\n%s", gotSniffed, len(wantCanonical) > 0, gotResp)
	}

	if !contains(gotResp, "Hello") {
		t.Fatalf("body missing from response:\n%s", gotResp)
	}
}

func TestResponseWriter_ReleaseClampsPendingCapacity(t *testing.T) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)

	rw := NewResponseWriter(ctx, nil)
	bigBody := bytes.Repeat([]byte("a"), pooledPendingCapLimit*2)
	if _, err := rw.Write(bigBody); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := cap(rw.pending); got <= pooledPendingCapLimit {
		t.Fatalf("pending cap = %d, want growth beyond clamp limit before release", got)
	}
	rw.Release()

	reused := NewResponseWriter(ctx, nil)
	defer reused.Release()
	if got := cap(reused.pending); got > pooledPendingCapLimit {
		t.Fatalf("pending cap after reuse = %d, want <= %d", got, pooledPendingCapLimit)
	}
}

func TestResponseWriter_ReleaseResetsOversizedHeaderMaps(t *testing.T) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)

	rw := NewResponseWriter(ctx, nil)
	for i := 0; i < pooledHeaderMapEntryLimit+1; i++ {
		key := "X-Test-" + strconv.Itoa(i)
		rw.Header().Set(key, "value")
		rw.Trailer().Set(key, "value")
	}
	oldHeaderPtr := reflect.ValueOf(rw.header).Pointer()
	oldTrailerPtr := reflect.ValueOf(rw.trailer).Pointer()
	rw.Release()

	reused := NewResponseWriter(ctx, nil)
	defer reused.Release()
	if len(reused.header) != 0 {
		t.Fatalf("header len after reuse = %d, want 0", len(reused.header))
	}
	if len(reused.trailer) != 0 {
		t.Fatalf("trailer len after reuse = %d, want 0", len(reused.trailer))
	}
	if got := reflect.ValueOf(reused.header).Pointer(); got == oldHeaderPtr {
		t.Fatal("expected oversized header map to be replaced on release")
	}
	if got := reflect.ValueOf(reused.trailer).Pointer(); got == oldTrailerPtr {
		t.Fatal("expected oversized trailer map to be replaced on release")
	}
}

func BenchmarkResponseWriterEnsureHeaderSentCommonCase(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := new(bytes.Buffer)
		bw := bufio.NewWriter(buf)
		ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)
		rw := NewResponseWriter(ctx, nil)
		rw.header.Set(headerContentType, "text/plain; charset=utf-8")
		rw.header.Set(headerContentLength, "2")

		if err := rw.ensureHeaderSent(); err != nil {
			b.Fatalf("ensureHeaderSent() error = %v", err)
		}

		rw.Release()
		ctx.Release()
	}
}

func BenchmarkResponseWriterEnsureHeaderSentCommonCaseDirectMapSetup(b *testing.B) {
	contentTypeValues := []string{"text/plain; charset=utf-8"}
	contentLengthValues := []string{"2"}

	for i := 0; i < b.N; i++ {
		buf := new(bytes.Buffer)
		bw := bufio.NewWriter(buf)
		ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)
		rw := NewResponseWriter(ctx, nil)
		rw.header[headerContentType] = contentTypeValues
		rw.header[headerContentLength] = contentLengthValues

		if err := rw.ensureHeaderSent(); err != nil {
			b.Fatalf("ensureHeaderSent() error = %v", err)
		}

		rw.Release()
		ctx.Release()
	}
}

func TestResponseWriterEnsureHeaderSentCommonCaseAllocationBudget(t *testing.T) {
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)
	contentTypeValues := []string{"text/plain; charset=utf-8"}
	contentLengthValues := []string{"2"}
	defer ctx.Release()

	allocs := testing.AllocsPerRun(1000, func() {
		buf.Reset()
		bw.Reset(&buf)
		rw := NewResponseWriter(ctx, nil)
		rw.header[headerContentType] = contentTypeValues
		rw.header[headerContentLength] = contentLengthValues

		if err := rw.ensureHeaderSent(); err != nil {
			t.Fatalf("ensureHeaderSent() error = %v", err)
		}

		rw.Release()
	})

	if allocs > 0 {
		t.Fatalf("allocs = %v, want <= 0", allocs)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && bytes.Contains([]byte(s), []byte(substr))
}
