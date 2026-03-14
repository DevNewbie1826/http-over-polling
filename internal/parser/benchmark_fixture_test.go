package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

var parserBenchmarkSetting = &Setting{
	MessageBegin:    func(*Parser, int) {},
	URL:             func(*Parser, []byte, int) {},
	Status:          func(*Parser, []byte, int) {},
	HeaderField:     func(*Parser, []byte, int) {},
	HeaderValue:     func(*Parser, []byte, int) {},
	HeadersComplete: func(*Parser, int) {},
	Body:            func(*Parser, []byte, int) {},
	MessageComplete: func(*Parser, int) {},
}

var parserBenchmarkSettingWithoutHeaderCallbacks = &Setting{
	MessageBegin:    func(*Parser, int) {},
	URL:             func(*Parser, []byte, int) {},
	Status:          func(*Parser, []byte, int) {},
	HeadersComplete: func(*Parser, int) {},
	Body:            func(*Parser, []byte, int) {},
	MessageComplete: func(*Parser, int) {},
}

type benchmarkHeaderScanBreakdown struct {
	headerLineCount                  int
	headerBytes                      int
	analyzeFastRequestPasses         int
	tryFastChunkedMetaPasses         int
	headerCallbackPasses             int
	totalEstimatedHeaderBytesScanned int
}

func httparserBenchmarkFixture() []byte {
	return []byte("POST /joyent/http-parser HTTP/1.1\r\n" +
		"Host: github.com\r\n" +
		"DNT: 1\r\n" +
		"Accept-Encoding: gzip, deflate, sdch\r\n" +
		"Accept-Language: ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4\r\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/39.0.2171.65 Safari/537.36\r\n" +
		"Accept: text/html,application/xhtml+xml,application/xml;q=0.9," +
		"image/webp,*/*;q=0.8\r\n" +
		"Referer: https://github.com/joyent/http-parser\r\n" +
		"Connection: keep-alive\r\n" +
		"Transfer-Encoding: chunked\r\n" +
		"Cache-Control: max-age=0\r\n\r\n" +
		"b\r\nhello world\r\n0\r\n\r\n")
}

func TestParserHttparserBenchmarkFixture(t *testing.T) {
	p := New(REQUEST)
	input := httparserBenchmarkFixture()

	consumed, err := p.Execute(&Setting{}, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	if !p.hasTransferEncoding {
		t.Fatal("hasTransferEncoding = false, want true")
	}

	if p.Method != POST {
		t.Fatalf("method = %v, want %v", p.Method, POST)
	}
}

func executeComparableBenchmarkOurParserWithSetting(input []byte, setting *Setting) error {
	p := New(REQUEST)
	consumed, err := p.Execute(setting, input)
	if err != nil {
		return err
	}
	if consumed != len(input) {
		return fmt.Errorf("our parser consumed %d bytes, want %d", consumed, len(input))
	}
	return nil
}

func executeComparableBenchmarkOurParser(input []byte) error {
	return executeComparableBenchmarkOurParserWithSetting(input, parserBenchmarkSetting)
}

func executeComparableBenchmarkNetHTTP(input []byte) error {
	_, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(input)))
	return err
}

func httparserBenchmarkHeaderScanBreakdown(input []byte, setting *Setting) (benchmarkHeaderScanBreakdown, error) {
	var breakdown benchmarkHeaderScanBreakdown

	c := newCursor(input)
	if _, ok := parseRequestLine(&c); !ok {
		return breakdown, fmt.Errorf("parseRequestLine failed")
	}
	lineEnd, ok := scanCRLF(&c)
	if !ok {
		return breakdown, fmt.Errorf("request line CRLF not found")
	}
	headersStart := c.pos + lineEnd + 2
	c.advance(lineEnd + 2)
	headersEnd := headersStart
	for {
		if c.remaining() < 2 {
			return breakdown, fmt.Errorf("incomplete headers")
		}
		if b, _ := c.peek(); b == '\r' {
			c.advance(1)
			if b2, ok := c.peek(); !ok || b2 != '\n' {
				return breakdown, fmt.Errorf("malformed header terminator")
			}
			headersEnd = c.pos - 1
			break
		}
		hLineEnd, ok := scanCRLF(&c)
		if !ok {
			return breakdown, fmt.Errorf("header CRLF not found")
		}
		if _, ok := parseHeaderLine(&c); !ok {
			return breakdown, fmt.Errorf("parseHeaderLine failed")
		}
		breakdown.headerLineCount++
		breakdown.headerBytes += hLineEnd + 2
		c.advance(hLineEnd + 2)
	}

	breakdown.analyzeFastRequestPasses = 1
	breakdown.tryFastChunkedMetaPasses = 1
	if setting != nil && (setting.HeaderField != nil || setting.HeaderValue != nil) {
		breakdown.headerCallbackPasses = 1
	}

	headerBlockBytes := headersEnd - headersStart
	if headerBlockBytes < 0 {
		headerBlockBytes = 0
	}
	breakdown.totalEstimatedHeaderBytesScanned = headerBlockBytes * (breakdown.analyzeFastRequestPasses + breakdown.tryFastChunkedMetaPasses + breakdown.headerCallbackPasses)

	return breakdown, nil
}

func TestComparableBenchmarkParsersConsumeFixture(t *testing.T) {
	input := httparserBenchmarkFixture()

	if err := executeComparableBenchmarkOurParser(input); err != nil {
		t.Fatalf("our parser error = %v", err)
	}
	if err := executeComparableBenchmarkNetHTTP(input); err != nil {
		t.Fatalf("net/http error = %v", err)
	}
}

func TestHttparserBenchmarkHeaderScanBreakdown(t *testing.T) {
	input := httparserBenchmarkFixture()

	withHeaders, err := httparserBenchmarkHeaderScanBreakdown(input, parserBenchmarkSetting)
	if err != nil {
		t.Fatalf("with header callbacks breakdown error = %v", err)
	}
	withoutHeaders, err := httparserBenchmarkHeaderScanBreakdown(input, parserBenchmarkSettingWithoutHeaderCallbacks)
	if err != nil {
		t.Fatalf("without header callbacks breakdown error = %v", err)
	}

	if withHeaders.headerLineCount != 10 {
		t.Fatalf("headerLineCount = %d, want 10", withHeaders.headerLineCount)
	}
	if withHeaders.analyzeFastRequestPasses != 1 {
		t.Fatalf("analyzeFastRequestPasses = %d, want 1", withHeaders.analyzeFastRequestPasses)
	}
	if withHeaders.tryFastChunkedMetaPasses != 1 {
		t.Fatalf("tryFastChunkedMetaPasses = %d, want 1", withHeaders.tryFastChunkedMetaPasses)
	}
	if withHeaders.headerCallbackPasses != 1 {
		t.Fatalf("headerCallbackPasses = %d, want 1", withHeaders.headerCallbackPasses)
	}
	if withoutHeaders.headerCallbackPasses != 0 {
		t.Fatalf("headerCallbackPasses without header callbacks = %d, want 0", withoutHeaders.headerCallbackPasses)
	}
	if withHeaders.totalEstimatedHeaderBytesScanned <= withoutHeaders.totalEstimatedHeaderBytesScanned {
		t.Fatalf("totalEstimatedHeaderBytesScanned with headers = %d, want > %d", withHeaders.totalEstimatedHeaderBytesScanned, withoutHeaders.totalEstimatedHeaderBytesScanned)
	}
}

func TestHttparserBenchmarkFixtureUsesStrictChunkedTermination(t *testing.T) {
	input := httparserBenchmarkFixture()
	if len(input) < 5 {
		t.Fatalf("fixture too short: %d", len(input))
	}
	if string(input[len(input)-5:]) != "0\r\n\r\n" {
		t.Fatalf("fixture tail = %q, want %q", string(input[len(input)-5:]), "0\r\n\r\n")
	}
}

func TestParserStrictFixtureHeaderCallbacks(t *testing.T) {
	p := New(REQUEST)
	input := httparserBenchmarkFixture()
	var events []string
	setting := &Setting{
		MessageBegin: func(*Parser, int) {
			events = append(events, "begin")
		},
		URL: func(*Parser, []byte, int) {
			events = append(events, "url")
		},
		HeaderField: func(_ *Parser, b []byte, _ int) {
			events = append(events, "hf:"+string(b))
		},
		HeaderValue: func(_ *Parser, b []byte, _ int) {
			events = append(events, "hv:"+string(b))
		},
		HeadersComplete: func(*Parser, int) {
			events = append(events, "headers")
		},
		Body: func(_ *Parser, b []byte, _ int) {
			events = append(events, "body:"+string(b))
		},
		MessageComplete: func(*Parser, int) {
			events = append(events, "done")
		},
	}

	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	want := []string{
		"begin",
		"url",
		"hf:Host", "hv:github.com",
		"hf:DNT", "hv:1",
		"hf:Accept-Encoding", "hv:gzip, deflate, sdch",
		"hf:Accept-Language", "hv:ru-RU,ru;q=0.8,en-US;q=0.6,en;q=0.4",
		"hf:User-Agent", "hv:Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.65 Safari/537.36",
		"hf:Accept", "hv:text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
		"hf:Referer", "hv:https://github.com/joyent/http-parser",
		"hf:Connection", "hv:keep-alive",
		"hf:Transfer-Encoding", "hv:chunked",
		"hf:Cache-Control", "hv:max-age=0",
		"headers",
		"body:hello world",
		"done",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestParserStrictFixtureCallbackPositions(t *testing.T) {
	p := New(REQUEST)
	input := httparserBenchmarkFixture()
	var positions []int
	setting := &Setting{
		MessageBegin: func(*Parser, int) {},
		URL: func(_ *Parser, _ []byte, pos int) {
			positions = append(positions, pos)
		},
		HeaderField: func(_ *Parser, _ []byte, pos int) {
			positions = append(positions, pos)
		},
		HeaderValue: func(_ *Parser, _ []byte, pos int) {
			positions = append(positions, pos)
		},
		HeadersComplete: func(_ *Parser, pos int) {
			positions = append(positions, pos)
		},
		Body: func(_ *Parser, _ []byte, pos int) {
			positions = append(positions, pos)
		},
		MessageComplete: func(_ *Parser, pos int) {
			positions = append(positions, pos)
		},
	}

	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	wantCount := 1 + 10 + 10 + 1 + 1 + 1
	if len(positions) != wantCount {
		t.Fatalf("positions len = %d, want %d", len(positions), wantCount)
	}
	for i, pos := range positions {
		if pos != len(input) {
			t.Fatalf("positions[%d] = %d, want %d", i, pos, len(input))
		}
	}
}

func TestParserStrictFixtureCallbacksAreZeroCopy(t *testing.T) {
	p := New(REQUEST)
	input := httparserBenchmarkFixture()
	var url, headerName, headerValue, body []byte
	setting := &Setting{
		URL: func(_ *Parser, b []byte, _ int) {
			if url == nil {
				url = b
			}
		},
		HeaderField: func(_ *Parser, b []byte, _ int) {
			if headerName == nil {
				headerName = b
			}
		},
		HeaderValue: func(_ *Parser, b []byte, _ int) {
			if headerValue == nil {
				headerValue = b
			}
		},
		Body: func(_ *Parser, b []byte, _ int) {
			if body == nil {
				body = b
			}
		},
	}

	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	assertAlias := func(name string, got []byte, want []byte) {
		t.Helper()
		if len(got) == 0 || len(want) == 0 {
			t.Fatalf("%s empty slice", name)
		}
		idx := bytes.Index(input, want)
		if idx < 0 {
			t.Fatalf("%s bytes %q not found in input", name, want)
		}
		if &got[0] != &input[idx] {
			t.Fatalf("%s is not zero-copy aliased into input", name)
		}
	}

	assertAlias("url", url, []byte("/joyent/http-parser"))
	assertAlias("headerName", headerName, []byte("Host"))
	assertAlias("headerValue", headerValue, []byte("github.com"))
	assertAlias("body", body, []byte("hello world"))
}
