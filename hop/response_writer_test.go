package hop

import (
	"bufio"
	"bytes"
	"net/http"
	"strings"
	"testing"
)

type flushBuffer struct {
	bytes.Buffer
}

func (f *flushBuffer) Flush() error { return nil }

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
