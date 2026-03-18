package adaptor

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/DevNewbie1826/http-over-polling/appcontext"
)

func newBenchmarkResponseWriter() (*ResponseWriter, *bytes.Buffer) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)
	return NewResponseWriter(ctx, nil), buf
}

func BenchmarkResponseWriter_ReadFrom_Chunked(b *testing.B) {
	payload := bytes.Repeat([]byte("a"), 32*1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rw, _ := newBenchmarkResponseWriter()
		// chunked path: no Content-Length set
		_, err := rw.ReadFrom(bytes.NewReader(payload))
		if err != nil {
			b.Fatalf("ReadFrom() error = %v", err)
		}
		if err := rw.EndResponse(); err != nil {
			b.Fatalf("EndResponse() error = %v", err)
		}
		rw.Release()
	}
}

func BenchmarkResponseWriter_ReadFrom_FixedLength(b *testing.B) {
	payload := bytes.Repeat([]byte("a"), 32*1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rw, _ := newBenchmarkResponseWriter()
		rw.Header().Set("Content-Length", "32768")
		_, err := rw.ReadFrom(bytes.NewReader(payload))
		if err != nil {
			b.Fatalf("ReadFrom() error = %v", err)
		}
		if err := rw.EndResponse(); err != nil {
			b.Fatalf("EndResponse() error = %v", err)
		}
		rw.Release()
	}
}

func BenchmarkResponseWriter_Write_SmallPayload(b *testing.B) {
	payload := []byte("hello")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rw, _ := newBenchmarkResponseWriter()
		if _, err := rw.Write(payload); err != nil {
			b.Fatalf("Write() error = %v", err)
		}
		if err := rw.EndResponse(); err != nil {
			b.Fatalf("EndResponse() error = %v", err)
		}
		rw.Release()
	}
}

func TestResponseWriter_ReadFrom_ChunkedFrameIntegrity(t *testing.T) {
	payload := []byte("chunked-payload")
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)
	rw := NewResponseWriter(ctx, nil)
	defer rw.Release()

	n, err := rw.ReadFrom(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("ReadFrom bytes = %d, want %d", n, len(payload))
	}
	if err := rw.EndResponse(); err != nil {
		t.Fatalf("EndResponse() error = %v", err)
	}

	resp := buf.String()
	if !bytes.Contains([]byte(resp), []byte("Transfer-Encoding: chunked")) {
		t.Fatalf("expected chunked response header, got:\n%s", resp)
	}
	if !bytes.Contains([]byte(resp), []byte("f\r\nchunked-payload\r\n")) {
		t.Fatalf("expected chunk frame, got:\n%s", resp)
	}
}

func TestResponseWriter_ReadFrom_FlushOrdering(t *testing.T) {
	buf := new(bytes.Buffer)
	bw := bufio.NewWriter(buf)
	ctx := appcontext.NewRequestContext(nil, context.Background(), nil, bw)
	rw := NewResponseWriter(ctx, nil)
	defer rw.Release()

	rw.WriteHeader(http.StatusCreated)
	if _, err := rw.ReadFrom(bytes.NewReader([]byte("abc"))); err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}
	if err := rw.EndResponse(); err != nil {
		t.Fatalf("EndResponse() error = %v", err)
	}

	out := buf.Bytes()
	statusIndex := bytes.Index(out, []byte("HTTP/1.1 201 Created"))
	bodyIndex := bytes.Index(out, []byte("abc"))
	if statusIndex < 0 || bodyIndex < 0 {
		t.Fatalf("missing status or body in response: %q", string(out))
	}
	if statusIndex > bodyIndex {
		t.Fatalf("status line appears after body: %q", string(out))
	}

	// ensure body is readable as stream and no write ordering corruption
	if _, err := io.Copy(io.Discard, bytes.NewReader(out)); err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}
}
