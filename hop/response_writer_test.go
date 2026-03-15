package hop

import (
	"bufio"
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/DevNewbie1826/http-over-polling/internal/bytebufferpool"
)

type flushBuffer struct {
	bytes.Buffer
}

func (f *flushBuffer) Flush() error { return nil }

type leaseCaptureBuffer struct {
	flushBuffer
	leaseWrites [][]byte
}

func (b *leaseCaptureBuffer) WriteLease(p []byte) (int, error) {
	b.leaseWrites = append(b.leaseWrites, append([]byte(nil), p...))
	return b.Write(p)
}

func TestResponseWriterRejectsNilWriter(t *testing.T) {
	writer := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
		request:    &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)},
	}
	if _, err := writer.Write([]byte("hello")); err == nil {
		t.Fatal("Write() error = nil, want non-nil for nil writer")
	}
	if err := writer.Close(); err == nil {
		t.Fatal("Close() error = nil, want non-nil for nil writer")
	}
}

func TestSingleWriteUsesContentLengthFastPath(t *testing.T) {
	request := &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
	buf := &flushBuffer{}
	writer := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
		request:    request,
		writer:     bufio.NewWriterSize(buf, 1024),
	}

	if _, err := writer.Write([]byte("hello world!")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Content-Length: 12\r\n") {
		t.Fatalf("response missing Content-Length fast path: %q", out)
	}
	if strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("response unexpectedly used chunked encoding: %q", out)
	}
	if !strings.HasSuffix(out, "\r\n\r\nhello world!") {
		t.Fatalf("response body suffix mismatch: %q", out)
	}
}

func TestPendingBodyDoesNotAliasCallerBuffer(t *testing.T) {
	request := &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
	buf := &flushBuffer{}
	writer := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
		request:    request,
		writer:     bufio.NewWriterSize(buf, 1024),
	}
	body := []byte("hello")
	if _, err := writer.Write(body); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	body[0] = 'j'
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\r\n\r\nhello") {
		t.Fatalf("response body aliased caller buffer: %q", buf.String())
	}
}

func TestResponseWriterHeaderInitializesAndReusesMap(t *testing.T) {
	w := &httpResponseWriter{}
	h1 := w.Header()
	if h1 == nil {
		t.Fatal("Header() returned nil")
	}
	h1.Set("X-Test", "ok")
	h2 := w.Header()
	if h2.Get("X-Test") != "ok" {
		t.Fatalf("Header() did not reuse header map, got X-Test=%q", h2.Get("X-Test"))
	}
}

func TestResponseWriterWriteHeaderSetsStatusCode(t *testing.T) {
	w := &httpResponseWriter{}
	w.WriteHeader(http.StatusCreated)
	if w.statusCode != http.StatusCreated {
		t.Fatalf("statusCode=%d, want %d", w.statusCode, http.StatusCreated)
	}
}

func TestWriteResponseHeaderAddsDefaultContentTypeDateAndChunked(t *testing.T) {
	buf := &flushBuffer{}
	w := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
	}

	chunked, err := w.writeResponseHeader(&http.Request{Close: true}, buf)
	if err != nil {
		t.Fatalf("writeResponseHeader() error = %v", err)
	}
	if !chunked {
		t.Fatal("chunked=false, want true when no Content-Length/Transfer-Encoding")
	}
	out := buf.String()
	if !strings.Contains(out, "HTTP/1.1 200 OK\r\n") {
		t.Fatalf("missing status line: %q", out)
	}
	if !strings.Contains(out, "Connection: close\r\n") {
		t.Fatalf("missing Connection close header: %q", out)
	}
	if !strings.Contains(out, "Content-Type: text/plain; charset=utf-8\r\n") {
		t.Fatalf("missing default Content-Type: %q", out)
	}
	if !strings.Contains(out, "Date: ") {
		t.Fatalf("missing default Date header: %q", out)
	}
	if !strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("missing default chunked transfer-encoding: %q", out)
	}
}

func TestWriteResponseHeaderWithContentLengthDisablesChunked(t *testing.T) {
	buf := &flushBuffer{}
	w := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusAccepted,
		header:     make(http.Header),
	}
	w.header.Set("Content-Length", "5")

	chunked, err := w.writeResponseHeader(&http.Request{}, buf)
	if err != nil {
		t.Fatalf("writeResponseHeader() error = %v", err)
	}
	if chunked {
		t.Fatal("chunked=true, want false when Content-Length is present")
	}
	out := buf.String()
	if strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("unexpected chunked transfer-encoding: %q", out)
	}
	if !strings.Contains(out, "Content-Length: 5\r\n") {
		t.Fatalf("missing Content-Length header: %q", out)
	}
}

func TestFlushPendingBodyForceChunkedFallback(t *testing.T) {
	buf := &flushBuffer{}
	w := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
		request:    &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)},
		writer:     buf,
	}

	if _, err := w.Write([]byte("A")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if _, err := w.Write([]byte("B")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("missing chunked transfer-encoding: %q", out)
	}
	if strings.Contains(out, "Content-Length:") {
		t.Fatalf("unexpected Content-Length in chunked fallback path: %q", out)
	}
	if !strings.HasSuffix(out, "\r\n\r\n1\r\nA\r\n1\r\nB\r\n0\r\n\r\n") {
		t.Fatalf("unexpected chunked body payload: %q", out)
	}
}

func TestFlushPendingBodySetsContentLengthWhenDirectPathDisabled(t *testing.T) {
	buf := &flushBuffer{}
	w := &httpResponseWriter{
		protoMajor:  1,
		protoMinor:  1,
		statusCode:  http.StatusOK,
		request:     &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)},
		writer:      buf,
		header:      make(http.Header),
		pendingBody: []byte("hello"),
	}
	w.header.Set("X-Test", "1")

	if err := w.flushPendingBody(false); err != nil {
		t.Fatalf("flushPendingBody() error = %v", err)
	}
	if got := w.Header().Get("Content-Length"); got != "5" {
		t.Fatalf("Content-Length=%q, want 5", got)
	}
	out := buf.String()
	if strings.Contains(out, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("unexpected chunked transfer-encoding: %q", out)
	}
	if !strings.HasSuffix(out, "\r\n\r\nhello") {
		t.Fatalf("response body suffix mismatch: %q", out)
	}
}

func TestWriteResponseHeaderConnWriteFlusherPathBuildsBufferedHeader(t *testing.T) {
	conn := newTestConn(nil)
	flusher := &connWriteFlusher{conn: conn, buf: bytebufferpool.Get(), lease: &connWriteLease{}}
	defer bytebufferpool.Put(flusher.buf)

	w := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusCreated,
		header:     make(http.Header),
	}
	w.header.Set("Content-Length", "4")
	w.header.Set("X-Test", "ok")

	chunked, err := w.writeResponseHeader(&http.Request{}, flusher)
	if err != nil {
		t.Fatalf("writeResponseHeader() error = %v", err)
	}
	if chunked {
		t.Fatal("chunked=true, want false when Content-Length is present")
	}
	got := string(flusher.buf.B)
	if !strings.Contains(got, "HTTP/1.1 201 Created\r\n") {
		t.Fatalf("missing status line: %q", got)
	}
	if !strings.Contains(got, "Content-Length: 4\r\n") {
		t.Fatalf("missing Content-Length header: %q", got)
	}
	if !strings.Contains(got, "X-Test: ok\r\n") {
		t.Fatalf("missing custom header: %q", got)
	}
	if strings.Contains(got, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("unexpected chunked transfer-encoding: %q", got)
	}
}

func TestWriteResponseHeaderConnWriteFlusherDefaultsToChunked(t *testing.T) {
	conn := newTestConn(nil)
	flusher := &connWriteFlusher{conn: conn, buf: bytebufferpool.Get(), lease: &connWriteLease{}}
	defer bytebufferpool.Put(flusher.buf)

	w := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
	}

	chunked, err := w.writeResponseHeader(&http.Request{Close: true}, flusher)
	if err != nil {
		t.Fatalf("writeResponseHeader() error = %v", err)
	}
	if !chunked {
		t.Fatal("chunked=false, want true")
	}
	got := string(flusher.buf.B)
	if !strings.Contains(got, "Connection: close\r\n") {
		t.Fatalf("missing Connection close header: %q", got)
	}
	if !strings.Contains(got, "Transfer-Encoding: chunked\r\n") {
		t.Fatalf("missing chunked transfer-encoding: %q", got)
	}
	if !strings.Contains(got, "Content-Type: text/plain; charset=utf-8\r\n") {
		t.Fatalf("missing default Content-Type: %q", got)
	}
}

func TestWriteDirectContentLengthResponseUsesConnWriteFlusherLeasePath(t *testing.T) {
	conn := newTestConn(nil)
	flusher := &connWriteFlusher{conn: conn, buf: bytebufferpool.Get(), lease: &connWriteLease{}}
	defer bytebufferpool.Put(flusher.buf)

	w := &httpResponseWriter{
		protoMajor:  1,
		protoMinor:  1,
		statusCode:  http.StatusAccepted,
		request:     &http.Request{Close: true},
		writer:      flusher,
		pendingBody: []byte("body"),
	}

	if err := w.writeDirectContentLengthResponse(); err != nil {
		t.Fatalf("writeDirectContentLengthResponse() error = %v", err)
	}
	if conn.writeHeaderCalls != 1 {
		t.Fatalf("WriteHeaderAndLease calls = %d, want 1", conn.writeHeaderCalls)
	}
	if conn.writeLeaseCalls != 1 {
		t.Fatalf("WriteLease calls = %d, want 1", conn.writeLeaseCalls)
	}
	got := conn.out.String()
	if !strings.Contains(got, "HTTP/1.1 202 Accepted\r\n") {
		t.Fatalf("missing status line: %q", got)
	}
	if !strings.Contains(got, "Connection: close\r\n") {
		t.Fatalf("missing Connection close header: %q", got)
	}
	if !strings.Contains(got, "Content-Length: 4\r\n\r\nbody") {
		t.Fatalf("missing content-length body output: %q", got)
	}
}

func TestWriteDirectContentLengthResponseUsesGenericLeaseWriter(t *testing.T) {
	buf := &leaseCaptureBuffer{}
	w := &httpResponseWriter{
		protoMajor:  1,
		protoMinor:  1,
		statusCode:  http.StatusOK,
		request:     &http.Request{},
		writer:      buf,
		pendingBody: []byte("lease"),
	}

	if err := w.writeDirectContentLengthResponse(); err != nil {
		t.Fatalf("writeDirectContentLengthResponse() error = %v", err)
	}
	if len(buf.leaseWrites) != 1 || string(buf.leaseWrites[0]) != "lease" {
		t.Fatalf("lease writes = %q, want [lease]", buf.leaseWrites)
	}
	if out := buf.String(); !strings.Contains(out, "Content-Length: 5\r\n\r\nlease") {
		t.Fatalf("output = %q, want content-length response with leased body", out)
	}
}
