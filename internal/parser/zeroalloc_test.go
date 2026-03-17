package parser

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

func runAllocs(t *testing.T, fn func() error) float64 {
	t.Helper()
	var runErr error
	allocs := testing.AllocsPerRun(1000, func() {
		if err := fn(); err != nil {
			runErr = err
		}
	})
	if runErr != nil {
		t.Fatalf("unexpected Execute() error = %v", runErr)
	}
	return allocs
}

func runReadRequestAllocs(t *testing.T, raw []byte, readBody bool) float64 {
	t.Helper()
	return runAllocs(t, func() error {
		req, err := ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil {
			return err
		}
		if readBody && req.Body != nil {
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				return err
			}
		}
		if req.Body != nil {
			return req.Body.Close()
		}
		return nil
	})
}

func TestReadRequestFixtureAllocations(t *testing.T) {
	allocs := runReadRequestAllocs(t, httparserBenchmarkFixture(), true)

	if allocs > 42 {
		t.Fatalf("allocs = %v, want <= 42", allocs)
	}
}

func TestReadRequestFixtureAllocations_ReducedHeaderPath(t *testing.T) {
	allocs := runReadRequestAllocs(t, httparserBenchmarkFixture(), true)

	if allocs > 39 {
		t.Fatalf("allocs = %v, want <= 39", allocs)
	}
}

func TestReadRequestSimpleGETAllocations(t *testing.T) {
	raw := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	allocs := runReadRequestAllocs(t, raw, false)

	if allocs > 7 {
		t.Fatalf("allocs = %v, want <= 7", allocs)
	}
}

func TestParserSimpleGETAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{}
	input := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserContentLengthBodyAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{}
	input := []byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 5\r\n\r\nhello")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitRequestLineAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{}
	part1 := []byte("GET /hel")
	part2 := []byte("lo HTTP/1.1\r\nHost: example.com\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitHeaderValueAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{}
	part1 := []byte("GET / HTTP/1.1\r\nHost: exa")
	part2 := []byte("mple.com\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitContentLengthBodyAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{}
	part1 := []byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\n\r\nhe")
	part2 := []byte("llo")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserResponseNoBodyAllocations(t *testing.T) {
	p := New(RESPONSE)
	setting := &Setting{}
	input := []byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserBothResponseNoBodyAllocations(t *testing.T) {
	p := New(BOTH)
	setting := &Setting{}
	input := []byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserChunkedBodyAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{}
	input := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSimpleGETCallbackAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{
		MessageBegin:    func(*Parser, int) {},
		URL:             func(*Parser, []byte, int) {},
		HeaderField:     func(*Parser, []byte, int) {},
		HeaderValue:     func(*Parser, []byte, int) {},
		HeadersComplete: func(*Parser, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	input := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserResponseNoBodyCallbackAllocations(t *testing.T) {
	p := New(RESPONSE)
	setting := &Setting{
		MessageBegin:    func(*Parser, int) {},
		Status:          func(*Parser, []byte, int) {},
		HeaderField:     func(*Parser, []byte, int) {},
		HeaderValue:     func(*Parser, []byte, int) {},
		HeadersComplete: func(*Parser, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	input := []byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserChunkedBodyCallbackAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{
		MessageBegin:    func(*Parser, int) {},
		HeaderField:     func(*Parser, []byte, int) {},
		HeaderValue:     func(*Parser, []byte, int) {},
		HeadersComplete: func(*Parser, int) {},
		Body:            func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	input := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitRequestLineCallbackAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{
		URL:             func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	part1 := []byte("GET /hel")
	part2 := []byte("lo HTTP/1.1\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitHeaderValueCallbackAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{
		HeaderValue:     func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	part1 := []byte("GET / HTTP/1.1\r\nHost: exa")
	part2 := []byte("mple.com\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitResponseLineCallbackAllocations(t *testing.T) {
	p := New(RESPONSE)
	setting := &Setting{
		Status:          func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	part1 := []byte("HTTP/1.1 204 No C")
	part2 := []byte("ontent\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitContentLengthBodyCallbackAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{
		Body:            func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	part1 := []byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\n\r\nhe")
	part2 := []byte("llo")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserSplitChunkedBodyCallbackAllocations(t *testing.T) {
	p := New(REQUEST)
	setting := &Setting{
		Body:            func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	part1 := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhe")
	part2 := []byte("llo\r\n0\r\n\r\n")

	allocs := runAllocs(t, func() error {
		p.Reset()
		if _, err := p.Execute(setting, part1); err != nil {
			return err
		}
		_, err := p.Execute(setting, part2)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserResponseContentLengthBodyAllocations(t *testing.T) {
	p := New(RESPONSE)
	setting := &Setting{}
	input := []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestParserResponseContentLengthBodyCallbackAllocations(t *testing.T) {
	p := New(RESPONSE)
	setting := &Setting{
		Status:          func(*Parser, []byte, int) {},
		HeaderField:     func(*Parser, []byte, int) {},
		HeaderValue:     func(*Parser, []byte, int) {},
		HeadersComplete: func(*Parser, int) {},
		Body:            func(*Parser, []byte, int) {},
		MessageComplete: func(*Parser, int) {},
	}
	input := []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")

	allocs := runAllocs(t, func() error {
		p.Reset()
		_, err := p.Execute(setting, input)
		return err
	})

	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
