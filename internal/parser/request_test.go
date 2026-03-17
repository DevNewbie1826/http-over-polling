package parser

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func readRequestWithStdlib(t *testing.T, raw []byte) *http.Request {
	t.Helper()

	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("http.ReadRequest() error = %v", err)
	}
	return req
}

func readRequestWithInternal(t *testing.T, raw []byte) *http.Request {
	t.Helper()

	req, err := ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadRequest() error = %v", err)
	}
	return req

}

func assertRequestParity(t *testing.T, got, want *http.Request) {
	t.Helper()

	if got.Method != want.Method {
		t.Fatalf("Method = %q, want %q", got.Method, want.Method)
	}
	if got.RequestURI != want.RequestURI {
		t.Fatalf("RequestURI = %q, want %q", got.RequestURI, want.RequestURI)
	}
	if got.Host != want.Host {
		t.Fatalf("Host = %q, want %q", got.Host, want.Host)
	}
	if got.Close != want.Close {
		t.Fatalf("Close = %v, want %v", got.Close, want.Close)
	}
	if got.ContentLength != want.ContentLength {
		t.Fatalf("ContentLength = %d, want %d", got.ContentLength, want.ContentLength)
	}
	if !reflect.DeepEqual(got.TransferEncoding, want.TransferEncoding) {
		t.Fatalf("TransferEncoding = %#v, want %#v", got.TransferEncoding, want.TransferEncoding)
	}
	if !reflect.DeepEqual(got.Header, want.Header) {
		t.Fatalf("Header = %#v, want %#v", got.Header, want.Header)
	}
	if got.URL.String() != want.URL.String() {
		t.Fatalf("URL.String() = %q, want %q", got.URL.String(), want.URL.String())
	}
	if got.URL.Scheme != want.URL.Scheme {
		t.Fatalf("URL.Scheme = %q, want %q", got.URL.Scheme, want.URL.Scheme)
	}
	if got.URL.Host != want.URL.Host {
		t.Fatalf("URL.Host = %q, want %q", got.URL.Host, want.URL.Host)
	}
	if got.URL.Path != want.URL.Path {
		t.Fatalf("URL.Path = %q, want %q", got.URL.Path, want.URL.Path)
	}
	if got.URL.RawQuery != want.URL.RawQuery {
		t.Fatalf("URL.RawQuery = %q, want %q", got.URL.RawQuery, want.URL.RawQuery)
	}
}

func TestReadRequest_ContentLengthBody(t *testing.T) {
	raw := []byte("POST /submit?debug=1 HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Content-Length: 5\r\n" +
		"X-Test: value\r\n\r\n" +
		"hello")

	req, err := ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadRequest() error = %v", err)
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
	if req.ContentLength != 5 {
		t.Fatalf("ContentLength = %d, want 5", req.ContentLength)
	}
	if got := req.Header.Get("X-Test"); got != "value" {
		t.Fatalf("Header.Get(X-Test) = %q, want %q", got, "value")
	}
	if got := req.Header.Get("Host"); got != "" {
		t.Fatalf("Header.Get(Host) = %q, want empty", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want %q", string(body), "hello")
	}
}

func TestReadRequest_ChunkedBody(t *testing.T) {
	raw := []byte("POST /chunked HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Transfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n")

	req, err := ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadRequest() error = %v", err)
	}

	if req.ContentLength != -1 {
		t.Fatalf("ContentLength = %d, want -1", req.ContentLength)
	}
	if len(req.TransferEncoding) != 1 || req.TransferEncoding[0] != "chunked" {
		t.Fatalf("TransferEncoding = %#v, want []string{\"chunked\"}", req.TransferEncoding)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q, want %q", string(body), "hello world")
	}
}

func TestReadRequest_HTTP10CloseSemantics(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{
			name: "http10 defaults close",
			raw:  []byte("GET / HTTP/1.0\r\nHost: example.com\r\n\r\n"),
		},
		{
			name: "http10 keep alive override",
			raw:  []byte("GET / HTTP/1.0\r\nHost: example.com\r\nConnection: keep-alive\r\n\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readRequestWithInternal(t, tt.raw)
			want := readRequestWithStdlib(t, tt.raw)
			assertRequestParity(t, got, want)
		})
	}
}

func TestReadRequest_ConnectionHeaderTokenParity(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{
			name: "http11 close token among others",
			raw:  []byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive, close\r\n\r\n"),
		},
		{
			name: "http10 keep-alive token among others",
			raw:  []byte("GET / HTTP/1.0\r\nHost: example.com\r\nConnection: upgrade, keep-alive\r\n\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readRequestWithInternal(t, tt.raw)
			want := readRequestWithStdlib(t, tt.raw)
			assertRequestParity(t, got, want)
		})
	}
}

func TestShouldCloseAfterReadConnectionSemantics(t *testing.T) {
	tests := []struct {
		name               string
		major              uint8
		minor              uint8
		hasClose           bool
		hasKeepAlive       bool
		wantCloseAfterRead bool
	}{
		{name: "http11 default keep alive", major: 1, minor: 1, wantCloseAfterRead: false},
		{name: "http11 connection close", major: 1, minor: 1, hasClose: true, wantCloseAfterRead: true},
		{name: "http10 default close", major: 1, minor: 0, wantCloseAfterRead: true},
		{name: "http10 keep alive opt in", major: 1, minor: 0, hasKeepAlive: true, wantCloseAfterRead: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldCloseAfterReadConnection(tt.major, tt.minor, tt.hasClose, tt.hasKeepAlive)
			if got != tt.wantCloseAfterRead {
				t.Fatalf("shouldCloseAfterReadConnection() = %v, want %v", got, tt.wantCloseAfterRead)
			}
		})
	}
}

func TestCanonicalHeaderKeyBytes(t *testing.T) {
	tests := []struct {
		name []byte
		want string
	}{
		{name: []byte("x-forwarded-for"), want: "X-Forwarded-For"},
		{name: []byte("ETAG"), want: "Etag"},
		{name: []byte("content-md5"), want: "Content-Md5"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := canonicalHeaderKeyBytes(tt.name); got != tt.want {
				t.Fatalf("canonicalHeaderKeyBytes(%q) = %q, want %q", string(tt.name), got, tt.want)
			}
		})
	}
}

func TestReadRequest_AbsoluteFormURLHostWinsOverHostHeader(t *testing.T) {
	raw := []byte("GET http://url.example.com/path?q=1 HTTP/1.1\r\nHost: header.example.com\r\n\r\n")

	got := readRequestWithInternal(t, raw)
	want := readRequestWithStdlib(t, raw)
	assertRequestParity(t, got, want)
}

func TestReadRequest_UnknownHeaderCanonicalizationParity(t *testing.T) {
	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\nX-FORWARDED-fOr: 127.0.0.1\r\nETAG: abc\r\n\r\n")

	got := readRequestWithInternal(t, raw)
	want := readRequestWithStdlib(t, raw)
	assertRequestParity(t, got, want)
}

func TestReadRequest_CONNECTAuthorityAndSlashFormParity(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{
			name: "authority form",
			raw:  []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: proxy.example.com\r\n\r\n"),
		},
		{
			name: "slash form",
			raw:  []byte("CONNECT /tunnel HTTP/1.1\r\nHost: proxy.example.com\r\n\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readRequestWithInternal(t, tt.raw)
			want := readRequestWithStdlib(t, tt.raw)
			assertRequestParity(t, got, want)
		})
	}
}

func TestReadRequest_LeavesPipelinedBytesReadableAfterBody(t *testing.T) {
	raw := []byte("POST /first HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhelloGET /next HTTP/1.1\r\nHost: example.com\r\n\r\n")
	reader := bufio.NewReader(bytes.NewReader(raw))

	first, err := ReadRequest(reader)
	if err != nil {
		t.Fatalf("ReadRequest(first) error = %v", err)
	}
	body, err := io.ReadAll(first.Body)
	if err != nil {
		t.Fatalf("ReadAll(first.Body) error = %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("first body = %q, want %q", string(body), "hello")
	}

	second, err := ReadRequest(reader)
	if err != nil {
		t.Fatalf("ReadRequest(second) error = %v", err)
	}
	if second.Method != http.MethodGet {
		t.Fatalf("second Method = %q, want %q", second.Method, http.MethodGet)
	}
	if second.URL.Path != "/next" {
		t.Fatalf("second URL.Path = %q, want %q", second.URL.Path, "/next")
	}
}

func benchmarkReadRequest(b *testing.B, raw []byte, drainBody bool) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))

	for i := 0; i < b.N; i++ {
		req, err := ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil {
			b.Fatalf("ReadRequest() error = %v", err)
		}
		if drainBody && req.Body != nil {
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				b.Fatalf("io.Copy(req.Body) error = %v", err)
			}
		}
		if req.Body != nil {
			_ = req.Body.Close()
		}
	}
}

func BenchmarkReadRequest_SimpleGET(b *testing.B) {
	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: benchmark\r\n\r\n")
	benchmarkReadRequest(b, raw, false)
}

func BenchmarkReadRequest_ContentLengthBody(b *testing.B) {
	raw := []byte("POST /submit HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	benchmarkReadRequest(b, raw, true)
}

func BenchmarkReadRequest_ChunkedBody(b *testing.B) {
	raw := []byte("POST /chunked HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")
	benchmarkReadRequest(b, raw, true)
}

func BenchmarkReadRequest_HttparserBenchmarkFixture(b *testing.B) {
	benchmarkReadRequest(b, httparserBenchmarkFixture(), true)
}

func BenchmarkParseRequestURL_QueryFastPath(b *testing.B) {
	requestURI := "/search?q=golang&lang=en"
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		u, err := parseRequestURL(GET, requestURI)
		if err != nil {
			b.Fatalf("parseRequestURL() error = %v", err)
		}
		if u.Path != "/search" || u.RawQuery != "q=golang&lang=en" {
			b.Fatalf("URL = %#v", u)
		}
	}
}

func BenchmarkReadRequest_ChunkedBodyHotPath(b *testing.B) {
	raw := []byte("POST /chunked HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Transfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n")
	benchmarkReadRequest(b, raw, true)
}
