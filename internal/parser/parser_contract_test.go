package parser

import (
	"reflect"
	"testing"
)

func TestParserBothModeTreatsHEADAsRequest(t *testing.T) {
	p := New(BOTH)
	var events []string
	setting := &Setting{
		MessageBegin: func(*Parser, int) { events = append(events, "begin") },
		URL:          func(*Parser, []byte, int) { events = append(events, "url") },
		HeadersComplete: func(*Parser, int) {
			events = append(events, "headers")
		},
		MessageComplete: func(*Parser, int) { events = append(events, "done") },
	}

	consumed, err := p.Execute(setting, []byte("HEAD /health HTTP/1.1\r\n\r\n"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len("HEAD /health HTTP/1.1\r\n\r\n") {
		t.Fatalf("consumed = %d, want %d", consumed, len("HEAD /health HTTP/1.1\r\n\r\n"))
	}

	if p.Method != HEAD {
		t.Fatalf("method = %v, want %v", p.Method, HEAD)
	}

	if p.Major != 1 || p.Minor != 1 {
		t.Fatalf("version = %d.%d, want 1.1", p.Major, p.Minor)
	}

	wantEvents := []string{"begin", "url", "headers", "done"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
}

func TestParserBothModeStillParsesHTTPResponses(t *testing.T) {
	p := New(BOTH)
	var status []byte
	setting := &Setting{
		Status: func(_ *Parser, b []byte, _ int) {
			status = append(status[:0], b...)
		},
	}

	consumed, err := p.Execute(setting, []byte("HTTP/1.1 204 No Content\r\n\r\n"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len("HTTP/1.1 204 No Content\r\n\r\n") {
		t.Fatalf("consumed = %d, want %d", consumed, len("HTTP/1.1 204 No Content\r\n\r\n"))
	}

	if p.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", p.StatusCode)
	}

	if string(status) != "No Content" {
		t.Fatalf("reason = %q, want %q", string(status), "No Content")
	}
}

func TestParserRequestWithContentLengthBody(t *testing.T) {
	p := New(REQUEST)
	var body [][]byte
	var events []string
	setting := &Setting{
		MessageBegin: func(*Parser, int) { events = append(events, "begin") },
		Body: func(_ *Parser, b []byte, _ int) {
			body = append(body, append([]byte(nil), b...))
			events = append(events, "body")
		},
		MessageComplete: func(*Parser, int) { events = append(events, "done") },
	}

	input := []byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\n\r\nhello")
	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	if len(body) != 1 || string(body[0]) != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}

	wantEvents := []string{"begin", "body", "done"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
}

func TestParserRejectsInvalidContentLength(t *testing.T) {
	p := New(REQUEST)
	_, err := p.Execute(&Setting{}, []byte("POST /upload HTTP/1.1\r\nContent-Length: nope\r\n\r\n"))
	if err == nil {
		t.Fatal("expected invalid Content-Length error")
	}
}

func TestParserContentLengthZeroConsumesWholeMessage(t *testing.T) {
	p := New(REQUEST)
	var events []string
	setting := &Setting{
		MessageBegin: func(*Parser, int) { events = append(events, "begin") },
		HeadersComplete: func(*Parser, int) {
			events = append(events, "headers")
		},
		MessageComplete: func(*Parser, int) { events = append(events, "done") },
	}

	input := []byte("POST /empty HTTP/1.1\r\nContent-Length: 0\r\n\r\n")
	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	wantEvents := []string{"begin", "headers", "done"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
}

func TestParserResetClearsMethodAcrossMessages(t *testing.T) {
	p := New(BOTH)
	if _, err := p.Execute(&Setting{}, []byte("GET / HTTP/1.1\r\n\r\n")); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	if p.Method != GET {
		t.Fatalf("method after request = %v, want %v", p.Method, GET)
	}

	if _, err := p.Execute(&Setting{}, []byte("HTTP/1.1 204 No Content\r\n\r\n")); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	if p.Method != 0 {
		t.Fatalf("method after response = %v, want 0", p.Method)
	}
}

func TestParserConnectionHeaderUsesTokenSemantics(t *testing.T) {
	p := New(REQUEST)
	input := []byte("GET / HTTP/1.1\r\nConnection: disclose\r\n\r\n")
	if _, err := p.Execute(&Setting{}, input); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if p.hasConnectionClose {
		t.Fatal("hasConnectionClose = true, want false for non-token substring")
	}
}

func TestParserTransferEncodingUsesTokenSemantics(t *testing.T) {
	p := New(REQUEST)
	input := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: notchunked\r\n\r\n")
	if _, err := p.Execute(&Setting{}, input); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if p.hasTransferEncoding {
		t.Fatal("hasTransferEncoding = true, want false for non-token substring")
	}
}

func TestParserBothModeRequestUpgradeSetsUpgradeState(t *testing.T) {
	p := New(BOTH)
	var completed bool
	setting := &Setting{
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	input := []byte("GET /chat HTTP/1.1\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	if !completed {
		t.Fatal("MessageComplete was not called")
	}

	if !p.Upgrade {
		t.Fatal("Upgrade = false, want true")
	}

	if !p.ReadyUpgradeData() {
		t.Fatal("ReadyUpgradeData = false, want true")
	}
}

func TestParserChunkedTransferEncodingOverridesContentLength(t *testing.T) {
	p := New(REQUEST)
	var chunks [][]byte
	setting := &Setting{
		Body: func(_ *Parser, b []byte, _ int) {
			chunks = append(chunks, append([]byte(nil), b...))
		},
	}

	input := []byte("POST / HTTP/1.1\r\nContent-Length: 3\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n")
	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}

	if len(chunks) != 1 || string(chunks[0]) != "hello" {
		t.Fatalf("chunks = %q, want %q", chunks, "hello")
	}
}

func TestParserSplitRequestLineAcrossExecuteCalls(t *testing.T) {
	p := New(REQUEST)
	var urls [][]byte
	var completed bool
	setting := &Setting{
		URL: func(_ *Parser, b []byte, _ int) {
			urls = append(urls, append([]byte(nil), b...))
		},
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	consumed1, err := p.Execute(setting, []byte("GET /hel"))
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if consumed1 != len("GET /hel") {
		t.Fatalf("first consumed = %d, want %d", consumed1, len("GET /hel"))
	}
	if completed {
		t.Fatal("MessageComplete fired too early")
	}

	consumed2, err := p.Execute(setting, []byte("lo HTTP/1.1\r\n\r\n"))
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if consumed2 != len("lo HTTP/1.1\r\n\r\n") {
		t.Fatalf("second consumed = %d, want %d", consumed2, len("lo HTTP/1.1\r\n\r\n"))
	}
	if !completed {
		t.Fatal("MessageComplete did not fire")
	}

	got := make([]string, len(urls))
	for i, b := range urls {
		got[i] = string(b)
	}
	want := []string{"/hel", "lo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("urls = %v, want %v", got, want)
	}
}

func TestParserSplitHeaderValueAcrossExecuteCalls(t *testing.T) {
	p := New(REQUEST)
	var values [][]byte
	var completed bool
	setting := &Setting{
		HeaderValue: func(_ *Parser, b []byte, _ int) {
			values = append(values, append([]byte(nil), b...))
		},
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	first := []byte("GET / HTTP/1.1\r\nHost: exa")
	consumed1, err := p.Execute(setting, first)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if consumed1 != len("GET / HTTP/1.1\r\nHost: ") {
		t.Fatalf("first consumed = %d, want %d", consumed1, len("GET / HTTP/1.1\r\nHost: "))
	}
	if completed {
		t.Fatal("MessageComplete fired too early")
	}

	second := []byte("mple.com\r\n\r\n")
	consumed2, err := p.Execute(setting, second)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if consumed2 != len(second) {
		t.Fatalf("second consumed = %d, want %d", consumed2, len(second))
	}
	if !completed {
		t.Fatal("MessageComplete did not fire")
	}

	got := make([]string, len(values))
	for i, b := range values {
		got[i] = string(b)
	}
	want := []string{"exa", "mple.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
}

func TestParserSplitResponseLineAcrossExecuteCalls(t *testing.T) {
	p := New(RESPONSE)
	var statuses []string
	var completed bool
	setting := &Setting{
		Status: func(_ *Parser, b []byte, _ int) {
			statuses = append(statuses, string(b))
		},
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	consumed1, err := p.Execute(setting, []byte("HTTP/1.1 204 No C"))
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if consumed1 != len("HTTP/1.1 204 No C") {
		t.Fatalf("first consumed = %d, want %d", consumed1, len("HTTP/1.1 204 No C"))
	}
	if completed {
		t.Fatal("MessageComplete fired too early")
	}

	consumed2, err := p.Execute(setting, []byte("ontent\r\n\r\n"))
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if consumed2 != len("ontent\r\n\r\n") {
		t.Fatalf("second consumed = %d, want %d", consumed2, len("ontent\r\n\r\n"))
	}
	if !completed {
		t.Fatal("MessageComplete did not fire")
	}
	if !reflect.DeepEqual(statuses, []string{"No C", "ontent"}) {
		t.Fatalf("statuses = %v, want %v", statuses, []string{"No C", "ontent"})
	}
}

func TestParserRejectsDuplicateContentLength(t *testing.T) {
	p := New(REQUEST)
	_, err := p.Execute(&Setting{}, []byte("POST / HTTP/1.1\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\nA"))
	if err == nil {
		t.Fatal("expected duplicate Content-Length error")
	}
}

func TestParserZeroChunkWithoutTrailersCompletesMessage(t *testing.T) {
	p := New(REQUEST)
	var completed bool
	setting := &Setting{
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	input := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n")
	consumed, err := p.Execute(setting, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}
	if !completed {
		t.Fatal("MessageComplete did not fire")
	}
}

func TestParserSplitChunkedBodyAcrossExecuteCalls(t *testing.T) {
	p := New(REQUEST)
	var chunks []string
	var completed bool
	setting := &Setting{
		Body: func(_ *Parser, b []byte, _ int) {
			chunks = append(chunks, string(b))
		},
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	part1 := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhe")
	consumed1, err := p.Execute(setting, part1)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if consumed1 != len(part1) {
		t.Fatalf("first consumed = %d, want %d", consumed1, len(part1))
	}
	if completed {
		t.Fatal("MessageComplete fired too early")
	}

	part2 := []byte("llo\r\n0\r\n\r\n")
	consumed2, err := p.Execute(setting, part2)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if consumed2 != len(part2) {
		t.Fatalf("second consumed = %d, want %d", consumed2, len(part2))
	}
	if !completed {
		t.Fatal("MessageComplete did not fire")
	}
	if !reflect.DeepEqual(chunks, []string{"he", "llo"}) {
		t.Fatalf("chunks = %v, want %v", chunks, []string{"he", "llo"})
	}
}

func TestParserSplitContentLengthBodyAcrossExecuteCalls(t *testing.T) {
	p := New(REQUEST)
	var bodies []string
	var completed bool
	setting := &Setting{
		Body: func(_ *Parser, b []byte, _ int) {
			bodies = append(bodies, string(b))
		},
		MessageComplete: func(*Parser, int) {
			completed = true
		},
	}

	part1 := []byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\n\r\nhe")
	consumed1, err := p.Execute(setting, part1)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if consumed1 != len(part1) {
		t.Fatalf("first consumed = %d, want %d", consumed1, len(part1))
	}
	if completed {
		t.Fatal("MessageComplete fired too early")
	}

	part2 := []byte("llo")
	consumed2, err := p.Execute(setting, part2)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if consumed2 != len(part2) {
		t.Fatalf("second consumed = %d, want %d", consumed2, len(part2))
	}
	if !completed {
		t.Fatal("MessageComplete did not fire")
	}
	if !reflect.DeepEqual(bodies, []string{"he", "llo"}) {
		t.Fatalf("bodies = %v, want %v", bodies, []string{"he", "llo"})
	}
}
