package hop

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/DevNewbie1826/http-over-polling/internal/bytebufferpool"
	httpparser "github.com/DevNewbie1826/http-over-polling/internal/parser"
)

var benchmarkFirstRequestWindowInput = []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
var benchmarkSmallResponseBody = []byte("ok")
var benchmarkSingleChunkedBodyInput = []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")
var benchmarkMultiChunkedBodyInput = []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n6\r\nhello \r\n5\r\nworld\r\n0\r\n\r\n")

func benchmarkConn(input []byte) *testConn {
	return newTestConn(input)
}

func resetBenchmarkConn(conn *testConn, input []byte) {
	conn.in = input
	conn.out.Reset()
	conn.ctx = nil
	conn.closed = false
	conn.readLeaseCalls = 0
	conn.writeLeaseCalls = 0
	conn.writeHeaderCalls = 0
	conn.completeCalls = 0
}

func measureHotPathAllocs(tb testing.TB, input []byte, handler http.Handler) float64 {
	tb.Helper()
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	return testing.AllocsPerRun(1000, func() {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			tb.Fatalf("Serve() error = %v", err)
		}
	})
}

func measureColdStartAllocs(tb testing.TB, input []byte, handler http.Handler) float64 {
	tb.Helper()
	return testing.AllocsPerRun(1000, func() {
		conn := benchmarkConn(input)
		hc := NewHttpConn(conn, handler)
		if err := hc.Serve(); err != nil {
			tb.Fatalf("Serve() error = %v", err)
		}
	})
}

func benchmarkPooledCompositeBufferConn(input []byte, handler http.Handler) *HttpConn {
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	setting := hc.newParserSetting()
	origMessageBegin := setting.MessageBegin
	origMessageComplete := setting.MessageComplete
	var pooled *CompositeBuffer
	setting.MessageBegin = func(p *httpparser.Parser, n int) {
		if pooled != nil {
			pooled.Reset()
			compositeBufferPool.Put(pooled)
			pooled = nil
		}
		origMessageBegin(p, n)
	}
	setting.Body = func(p *httpparser.Parser, buf []byte, _ int) {
		hc := p.GetUserData().(*HttpConn)
		if pooled == nil && hc.body.Len() == 0 && hc.bodyView == nil {
			hc.bodyView = buf
			return
		}
		if pooled == nil {
			pooled = compositeBufferPool.Get().(*CompositeBuffer)
			pooled.Reset()
		}
		if hc.bodyView != nil {
			if _, err := pooled.WriteClone(hc.bodyView); err != nil {
				panic(err)
			}
			hc.bodyView = nil
		}
		if _, err := pooled.WriteClone(buf); err != nil {
			panic(err)
		}
	}
	setting.MessageComplete = func(p *httpparser.Parser, n int) {
		if pooled != nil {
			hc.body = *pooled
		}
		origMessageComplete(p, n)
		if pooled != nil {
			hc.body = CompositeBuffer{}
			pooled.Reset()
			compositeBufferPool.Put(pooled)
			pooled = nil
		}
	}
	hc.setting = setting
	return hc
}

func TestHopHotPathAndColdStartGETAllocations(t *testing.T) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	})

	hot := measureHotPathAllocs(t, input, handler)
	cold := measureColdStartAllocs(t, input, handler)
	if hot >= cold {
		t.Fatalf("hot-path allocs = %v, want less than cold-start allocs = %v", hot, cold)
	}
	if hot == 0 && cold == 0 {
		t.Fatal("hot and cold allocation baselines are both zero; expected measurable separation")
	}
	if got, want := len(input), 0; got == want {
		t.Fatal("unreachable guard")
	}
	if testing.Verbose() {
		t.Logf("GET hot allocs=%v cold allocs=%v", hot, cold)
	}
	_ = bytes.MinRead
	_ = io.Discard
}

func TestHopSingleChunkGETAllocationBudget(t *testing.T) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	})

	hot := measureHotPathAllocs(t, input, handler)
	budget := 0.0
	if raceSafeCopiesEnabled() {
		budget = 3.0
	}
	if hot > budget {
		t.Fatalf("GET hot-path allocs = %v, want <= %v", hot, budget)
	}
}

func TestHopHotPathAndColdStartBodyReadAllocations(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})

	hot := measureHotPathAllocs(t, input, handler)
	cold := measureColdStartAllocs(t, input, handler)
	if hot >= cold {
		t.Fatalf("hot-path allocs = %v, want less than cold-start allocs = %v", hot, cold)
	}
	if testing.Verbose() {
		t.Logf("BODY hot allocs=%v cold allocs=%v", hot, cold)
	}
}

func TestHopSingleChunkBodyDiscardAllocationBudget(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})

	hot := measureHotPathAllocs(t, input, handler)
	budget := 1.0
	if raceSafeCopiesEnabled() {
		budget = 7.0
	}
	if hot > budget {
		t.Fatalf("BODY-discard hot-path allocs = %v, want <= %v", hot, budget)
	}
}

func TestHopBodyReadAddsAllocationsBeyondUnreadBaseline(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	alreadyRead := measureHotPathAllocs(t, input, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	notRead := measureHotPathAllocs(t, input, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if alreadyRead <= notRead {
		t.Fatalf("already-read hot-path allocs = %v, want > unread allocs = %v", alreadyRead, notRead)
	}
}

func TestHopStoredContentLengthHeaderMatchesHostOnlyAllocationBudget(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Content-Length fast path budgeting")
	}
	withContentLength := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	withoutExtraHeader := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	withHeaderAllocs := measureHotPathAllocs(t, withContentLength, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if got := r.Header.Get("Content-Length"); got != "5" {
			t.Fatalf("Content-Length header = %q, want %q", got, "5")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	withoutHeaderAllocs := measureHotPathAllocs(t, withoutExtraHeader, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if withHeaderAllocs > withoutHeaderAllocs {
		t.Fatalf("content-length path allocs = %v, want <= host-only allocs = %v", withHeaderAllocs, withoutHeaderAllocs)
	}
}

func TestHopSingleChunkBodyDiscardCanReachZeroAllocBudget(t *testing.T) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	hot := measureHotPathAllocs(t, input, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Length"); got != "5" {
			t.Fatalf("Content-Length header = %q, want %q", got, "5")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	budget := 0.0
	if raceSafeCopiesEnabled() {
		budget = 6.0
	}
	if hot > budget {
		t.Fatalf("BODY-discard zero-alloc target allocs = %v, want <= %v", hot, budget)
	}
}

func TestHopStoredTransferEncodingHeaderMatchesChunkedDiscardZeroAllocBudget(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Transfer-Encoding fast path budgeting")
	}
	input := benchmarkSingleChunkedBodyInput
	hot := measureHotPathAllocs(t, input, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Transfer-Encoding"); got != "chunked" {
			t.Fatalf("Transfer-Encoding header = %q, want %q", got, "chunked")
		}
		if got := r.ContentLength; got != -1 {
			t.Fatalf("ContentLength = %d, want -1", got)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if hot > 0 {
		t.Fatalf("chunked discard hot-path allocs = %v, want 0", hot)
	}
}

func TestHopStoredTransferEncodingHeaderMatchesChunkedReadAllocationFloor(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Transfer-Encoding fast path budgeting")
	}
	chunkedAllocs := measureHotPathAllocs(t, benchmarkSingleChunkedBodyInput, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Transfer-Encoding"); got != "chunked" {
			t.Fatalf("Transfer-Encoding header = %q, want %q", got, "chunked")
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("payload = %q, want %q", string(payload), "hello")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	contentLengthAllocs := measureHotPathAllocs(t, []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("payload = %q, want %q", string(payload), "hello")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if chunkedAllocs > contentLengthAllocs {
		t.Fatalf("chunked read allocs = %v, want <= content-length read allocs = %v", chunkedAllocs, contentLengthAllocs)
	}
}

func TestHopStoredContentTypeHeaderMatchesContentLengthDiscardBudget(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Content-Type fast path budgeting")
	}
	withContentType := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
	withoutContentType := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	withHeaderAllocs := measureHotPathAllocs(t, withContentType, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "text/plain" {
			t.Fatalf("Content-Type header = %q, want %q", got, "text/plain")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	withoutHeaderAllocs := measureHotPathAllocs(t, withoutContentType, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if withHeaderAllocs > withoutHeaderAllocs {
		t.Fatalf("content-type path allocs = %v, want <= content-length-only allocs = %v", withHeaderAllocs, withoutHeaderAllocs)
	}
}

func TestHopStoredContentTypeHeaderMatchesContentLengthReadBudget(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Content-Type fast path budgeting")
	}
	withContentType := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
	withoutContentType := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	withHeaderAllocs := measureHotPathAllocs(t, withContentType, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "text/plain" {
			t.Fatalf("Content-Type header = %q, want %q", got, "text/plain")
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("payload = %q, want %q", string(payload), "hello")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	withoutHeaderAllocs := measureHotPathAllocs(t, withoutContentType, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("payload = %q, want %q", string(payload), "hello")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if withHeaderAllocs > withoutHeaderAllocs {
		t.Fatalf("content-type read allocs = %v, want <= content-length-only read allocs = %v", withHeaderAllocs, withoutHeaderAllocs)
	}
}

func TestHopStoredUpgradeHeaderMatchesGetBudget(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Upgrade fast path budgeting")
	}
	withUpgrade := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\nConnection: upgrade\r\nUpgrade: websocket\r\n\r\n")
	withoutUpgrade := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	withHeaderAllocs := measureHotPathAllocs(t, withUpgrade, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Upgrade"); got != "websocket" {
			t.Fatalf("Upgrade header = %q, want %q", got, "websocket")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	withoutHeaderAllocs := measureHotPathAllocs(t, withoutUpgrade, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if withHeaderAllocs > withoutHeaderAllocs {
		t.Fatalf("upgrade path allocs = %v, want <= get allocs = %v", withHeaderAllocs, withoutHeaderAllocs)
	}
}

func TestHopStoredTrailerHeaderMatchesChunkedDiscardBudget(t *testing.T) {
	if raceSafeCopiesEnabled() {
		t.Skip("race build adds body safety copies that obscure non-race Trailer fast path budgeting")
	}
	withTrailer := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\nTrailer: X-Foo\r\n\r\n")
	withoutTrailer := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	withHeaderAllocs := measureHotPathAllocs(t, withTrailer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Trailer"); got != "X-Foo" {
			t.Fatalf("Trailer header = %q, want %q", got, "X-Foo")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	withoutHeaderAllocs := measureHotPathAllocs(t, withoutTrailer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	}))
	if withHeaderAllocs > withoutHeaderAllocs {
		t.Fatalf("trailer path allocs = %v, want <= get allocs = %v", withHeaderAllocs, withoutHeaderAllocs)
	}
}

func TestReadLeaseBodyReadAllMatchesOwnedSliceAllocationFloor(t *testing.T) {
	leaseBodyAllocs := testing.AllocsPerRun(1000, func() {
		body := &readLeaseBody{view: []byte("hello")}
		payload, err := io.ReadAll(body)
		if err != nil {
			t.Fatalf("ReadAll(readLeaseBody) error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("ReadAll(readLeaseBody) = %q, want %q", string(payload), "hello")
		}
	})
	bytesReaderAllocs := testing.AllocsPerRun(1000, func() {
		payload, err := io.ReadAll(bytes.NewReader([]byte("hello")))
		if err != nil {
			t.Fatalf("ReadAll(bytes.Reader) error = %v", err)
		}
		if string(payload) != "hello" {
			t.Fatalf("ReadAll(bytes.Reader) = %q, want %q", string(payload), "hello")
		}
	})
	if leaseBodyAllocs != bytesReaderAllocs {
		t.Fatalf("readLeaseBody ReadAll allocs = %v, want bytes.Reader allocs = %v", leaseBodyAllocs, bytesReaderAllocs)
	}
}

func TestHopGenericResponseWriteRemainsBudgeted(t *testing.T) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	})

	hot := measureHotPathAllocs(t, input, handler)
	budget := 0.0
	if raceSafeCopiesEnabled() {
		budget = 3.0
	}
	if hot > budget {
		t.Fatalf("generic response write hot-path allocs = %v, want <= %v budget", hot, budget)
	}
	if testing.Verbose() {
		t.Logf("generic response hot allocs=%v", hot)
	}
}

func TestResponseWriterSmallBodyInlineBufferAllocations(t *testing.T) {
	request := &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
	conn := newTestConn(nil)
	writer := &httpResponseWriter{}
	flusher := &connWriteFlusher{conn: conn, buf: bytebufferpool.Get(), lease: &connWriteLease{}}

	allocs := testing.AllocsPerRun(1000, func() {
		conn.out.Reset()
		flusher.buf.Reset()
		writer.protoMajor = 1
		writer.protoMinor = 1
		writer.statusCode = http.StatusOK
		writer.request = request
		writer.writer = flusher
		writer.headerWritten = false
		writer.chunkWriter = nil
		writer.pendingBody = nil
		if _, err := writer.Write(benchmarkSmallResponseBody); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	if allocs != 0 {
		t.Fatalf("small response write allocs = %v, want 0 with inline staging", allocs)
	}
}

func BenchmarkHopSingleChunkGETHotPath(b *testing.B) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopPipelinedGETSameReadinessDrainExperiment(b *testing.B) {
	input := []byte("GET /one HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\n\r\nGET /two HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		calls, err := serveUntilInputDrainedForTest(hc, conn, 4)
		if err != nil {
			b.Fatalf("serveUntilInputDrainedForTest() error = %v", err)
		}
		if calls != 1 {
			b.Fatalf("Serve() calls = %d, want 1 after single-dispatch drain", calls)
		}
	}
}

func BenchmarkHopSingleChunkGETColdStart(b *testing.B) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn := benchmarkConn(input)
		hc := NewHttpConn(conn, handler)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkBodyReadHotPath(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkContentTypeBodyReadHotPath(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "text/plain" {
			b.Fatalf("Content-Type header = %q, want %q", got, "text/plain")
		}
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopUpgradeGETHotPath(b *testing.B) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\nConnection: upgrade\r\nUpgrade: websocket\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Upgrade"); got != "websocket" {
			b.Fatalf("Upgrade header = %q, want %q", got, "websocket")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkChunkedBodyReadHotPath(b *testing.B) {
	input := benchmarkSingleChunkedBodyInput
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			b.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello" {
			b.Fatalf("payload = %q, want %q", string(payload), "hello")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkBodyDiscardHotPath(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkContentTypeBodyDiscardHotPath(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "text/plain" {
			b.Fatalf("Content-Type header = %q, want %q", got, "text/plain")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkTrailerBodyDiscardHotPath(b *testing.B) {
	input := []byte("GET /hello HTTP/1.1\r\nHost: example.com\r\nTrailer: X-Foo\r\n\r\n")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Trailer"); got != "X-Foo" {
			b.Fatalf("Trailer header = %q, want %q", got, "X-Foo")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkChunkedBodyDiscardHotPath(b *testing.B) {
	input := benchmarkSingleChunkedBodyInput
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopSingleChunkBodyReadColdStart(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn := benchmarkConn(input)
		hc := NewHttpConn(conn, handler)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopMultiChunkedBodyReadHotPath(b *testing.B) {
	input := benchmarkMultiChunkedBodyInput
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			b.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello world" {
			b.Fatalf("payload = %q, want %q", string(payload), "hello world")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopMultiChunkedBodyDiscardHotPath(b *testing.B) {
	input := benchmarkMultiChunkedBodyInput
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	conn := benchmarkConn(input)
	hc := NewHttpConn(conn, handler)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopMultiChunkedBodyReadHotPathPooledCompositeBuffer(b *testing.B) {
	input := benchmarkMultiChunkedBodyInput
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			b.Fatalf("ReadAll() error = %v", err)
		}
		if string(payload) != "hello world" {
			b.Fatalf("payload = %q, want %q", string(payload), "hello world")
		}
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	hc := benchmarkPooledCompositeBufferConn(input, handler)
	conn := hc.conn.(*testConn)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkHopMultiChunkedBodyDiscardHotPathPooledCompositeBuffer(b *testing.B) {
	input := benchmarkMultiChunkedBodyInput
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(benchmarkSmallResponseBody)
	})
	hc := benchmarkPooledCompositeBufferConn(input, handler)
	conn := hc.conn.(*testConn)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetBenchmarkConn(conn, input)
		if err := hc.Serve(); err != nil {
			b.Fatalf("Serve() error = %v", err)
		}
	}
}

func BenchmarkFirstRequestWindowGET(b *testing.B) {
	input := benchmarkFirstRequestWindowInput
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n := firstRequestWindow(input); n != len(input) {
			b.Fatalf("firstRequestWindow() = %d, want %d", n, len(input))
		}
	}
}

func BenchmarkFirstRequestWindowPOSTContentLength(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n := firstRequestWindow(input); n != len(input) {
			b.Fatalf("firstRequestWindow() = %d, want %d", n, len(input))
		}
	}
}

func BenchmarkWriteDirectContentLengthResponseColdStart(b *testing.B) {
	request := &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
	body := []byte("ok")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn := newTestConn(nil)
		writer := &httpResponseWriter{
			protoMajor: 1,
			protoMinor: 1,
			statusCode: http.StatusOK,
			request:    request,
			writer: &connWriteFlusher{
				conn:  conn,
				buf:   bytebufferpool.Get(),
				lease: &connWriteLease{},
			},
			pendingBody: body,
		}
		if err := writer.writeDirectContentLengthResponse(); err != nil {
			b.Fatalf("writeDirectContentLengthResponse() error = %v", err)
		}
	}
}

func BenchmarkWriteDirectContentLengthResponseHotPath(b *testing.B) {
	request := &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
	body := []byte("ok")
	conn := newTestConn(nil)
	flusher := &connWriteFlusher{conn: conn, buf: bytebufferpool.Get(), lease: &connWriteLease{}}
	writer := &httpResponseWriter{
		protoMajor:  1,
		protoMinor:  1,
		statusCode:  http.StatusOK,
		request:     request,
		writer:      flusher,
		pendingBody: body,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.out.Reset()
		flusher.buf.Reset()
		writer.pendingBody = body
		if err := writer.writeDirectContentLengthResponse(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteGenericHeaderResponseHotPath(b *testing.B) {
	request := &http.Request{ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header)}
	body := []byte("ok")
	conn := newTestConn(nil)
	flusher := &connWriteFlusher{conn: conn, buf: bytebufferpool.Get(), lease: &connWriteLease{}}
	writer := &httpResponseWriter{
		protoMajor: 1,
		protoMinor: 1,
		statusCode: http.StatusOK,
		request:    request,
		writer:     flusher,
		header: http.Header{
			"X-Test": []string{"yes"},
		},
		pendingBody: body,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.out.Reset()
		flusher.buf.Reset()
		writer.headerWritten = false
		writer.chunkWriter = nil
		writer.pendingBody = body
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
