package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"
)

func headerHeavyRequestFixture() []byte {
	var b bytes.Buffer
	b.WriteString("GET /alloc-heavy?x=1&y=2 HTTP/1.1\r\n")
	b.WriteString("Host: example.com\r\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "X-Debug-%02d: value-%02d\r\n", i, i)
	}
	b.WriteString("Connection: keep-alive\r\n")
	b.WriteString("\r\n")
	return b.Bytes()
}

func unknownHeaderHeavyRequestFixture() []byte {
	var b bytes.Buffer
	b.WriteString("GET /unknown-heavy?trace=true HTTP/1.1\r\n")
	b.WriteString("Host: example.com\r\n")
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&b, "X-Custom-Very-Long-Header-Name-%03d: alpha-beta-gamma-%03d\r\n", i, i)
	}
	b.WriteString("\r\n")
	return b.Bytes()
}

func queryHeavyRequestFixture() []byte {
	return []byte("GET /search?q=alpha&lang=en&sort=desc&page=10&filter=active&limit=100 HTTP/1.1\r\nHost: example.com\r\nAccept: */*\r\n\r\n")
}

func benchmarkReadRequestFromBytes(b *testing.B, fixture []byte) {
	b.Helper()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := bufio.NewReader(bytes.NewReader(fixture))
		req, err := ReadRequest(r)
		if err != nil {
			b.Fatalf("ReadRequest() error = %v", err)
		}
		if req.Body != nil {
			_ = req.Body.Close()
		}
	}
}

func BenchmarkReadRequest_HeaderHeavy(b *testing.B) {
	benchmarkReadRequestFromBytes(b, headerHeavyRequestFixture())
}

func BenchmarkReadRequest_UnknownHeaderHeavy(b *testing.B) {
	benchmarkReadRequestFromBytes(b, unknownHeaderHeavyRequestFixture())
}

func BenchmarkReadRequest_QueryHeavy(b *testing.B) {
	benchmarkReadRequestFromBytes(b, queryHeavyRequestFixture())
}
