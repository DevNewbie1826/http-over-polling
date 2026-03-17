package parser

import (
	"bufio"
	"bytes"
	"net/http"
	"testing"
)

func BenchmarkParserSimpleGET(b *testing.B) {
	input := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	p := New(REQUEST)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserContentLengthBody(b *testing.B) {
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")
	p := New(REQUEST)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserSplitRequestLine(b *testing.B) {
	part1 := []byte("GET /hel")
	part2 := []byte("lo HTTP/1.1\r\nHost: example.com\r\n\r\n")
	p := New(REQUEST)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			b.Fatal(err)
		}
		if _, err := p.Execute(setting, part2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserSplitHeaderValue(b *testing.B) {
	part1 := []byte("GET / HTTP/1.1\r\nHost: exa")
	part2 := []byte("mple.com\r\n\r\n")
	p := New(REQUEST)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			b.Fatal(err)
		}
		if _, err := p.Execute(setting, part2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserSplitContentLengthBody(b *testing.B) {
	part1 := []byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\n\r\nhe")
	part2 := []byte("llo")
	p := New(REQUEST)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			b.Fatal(err)
		}
		if _, err := p.Execute(setting, part2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserResponseNoBody(b *testing.B) {
	input := []byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
	p := New(RESPONSE)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserBothResponseNoBody(b *testing.B) {
	input := []byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
	p := New(BOTH)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserChunkedBody(b *testing.B) {
	input := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")
	p := New(REQUEST)
	setting := &Setting{}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserChunkedBodyCallbacks(b *testing.B) {
	input := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\nX-Test: abc\r\n\r\n5\r\nhello\r\n0\r\n\r\n")
	p := New(REQUEST)
	setting := &Setting{
		HeaderField:     func(*Parser, []byte, int) {},
		HeaderValue:     func(*Parser, []byte, int) {},
		HeadersComplete: func(*Parser, int) {},
		Body:            func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(setting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserHttparserBenchmarkFixture(b *testing.B) {
	input := httparserBenchmarkFixture()
	p := New(REQUEST)
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(parserBenchmarkSetting, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserHttparserBenchmarkFixtureNoHeaderCallbacks(b *testing.B) {
	input := httparserBenchmarkFixture()
	p := New(REQUEST)
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		p.Reset()
		if _, err := p.Execute(parserBenchmarkSettingWithoutHeaderCallbacks, input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTryFastChunkedRequestHttparserBenchmarkFixture(b *testing.B) {
	input := httparserBenchmarkFixture()
	p := New(REQUEST)
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		p.Reset()
		n, ok, err := p.tryFastChunkedRequest(parserBenchmarkSetting, input)
		if err != nil {
			b.Fatal(err)
		}
		if !ok {
			b.Fatal("tryFastChunkedRequest unexpectedly failed")
		}
		if n != len(input) {
			b.Fatalf("consumed = %d, want %d", n, len(input))
		}
	}
}

func BenchmarkNetHTTPHttparserBenchmarkFixture(b *testing.B) {
	input := httparserBenchmarkFixture()
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))

	for i := 0; i < b.N; i++ {
		if _, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(input))); err != nil {
			b.Fatal(err)
		}
	}
}
