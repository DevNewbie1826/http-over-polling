package parser

import (
	"reflect"
	"testing"
)

func TestTryFastRequestHandlesStrictChunkedFixture(t *testing.T) {
	p := New(REQUEST)
	input := httparserBenchmarkFixture()
	var body [][]byte
	setting := &Setting{
		HeaderField:     func(*Parser, []byte, int) {},
		HeaderValue:     func(*Parser, []byte, int) {},
		HeadersComplete: func(*Parser, int) {},
		Body: func(_ *Parser, b []byte, _ int) {
			body = append(body, append([]byte(nil), b...))
		},
		MessageComplete: func(*Parser, int) {},
	}

	consumed, ok, err := p.tryFastRequest(setting, input)
	if err != nil {
		t.Fatalf("tryFastRequest error = %v", err)
	}
	if !ok {
		t.Fatal("tryFastRequest ok = false, want true")
	}
	if consumed != len(input) {
		t.Fatalf("consumed = %d, want %d", consumed, len(input))
	}
	if len(body) != 1 || string(body[0]) != "hello world" {
		t.Fatalf("body = %q, want %q", body, "hello world")
	}
}

func TestHeaderSemanticErrorReportsSpecificCases(t *testing.T) {
	if err := headerSemanticError(messageMeta{hasContentLength: true}, headerLine{Name: []byte("Content-Length"), Value: []byte("2")}); err == nil || err.Error() != "duplicate Content-Length" {
		t.Fatalf("duplicate error = %v, want duplicate Content-Length", err)
	}
	if err := headerSemanticError(messageMeta{}, headerLine{Name: []byte("Content-Length"), Value: []byte("bad")}); err == nil || err.Error() != "invalid Content-Length: \"bad\"" {
		t.Fatalf("invalid content-length error = %v", err)
	}
	if err := headerSemanticError(messageMeta{}, headerLine{Name: []byte("X-Test"), Value: []byte("bad")}); err == nil || err.Error() != "invalid header semantics" {
		t.Fatalf("fallback error = %v, want invalid header semantics", err)
	}
}

func TestIncompleteConsumedForCurrentState(t *testing.T) {
	p := &Parser{currentType: REQUEST, pending: []byte("GET / HTTP/1.1\r\nHost: exa")}
	if got := incompleteConsumedForCurrentState(p); got != len("GET / HTTP/1.1\r\nHost: ") {
		t.Fatalf("incompleteConsumedForCurrentState() = %d", got)
	}

	p.pending = []byte("GET / HTTP/1.1")
	if got := incompleteConsumedForCurrentState(p); got != len(p.pending) {
		t.Fatalf("incompleteConsumedForCurrentState(no CRLF) = %d, want %d", got, len(p.pending))
	}

	p.currentType = RESPONSE
	if got := incompleteConsumedForCurrentState(p); got != -1 {
		t.Fatalf("incompleteConsumedForCurrentState(response) = %d, want -1", got)
	}
}

func TestApplyParsedMessageSetsUpgradeAndContentLength(t *testing.T) {
	p := New(BOTH)
	p.applyParsedMessage(parsedMessage{
		kind:    REQUEST,
		request: requestLine{Method: GET, Major: 1, Minor: 1},
		headers: []headerLine{{Name: []byte("Connection"), Value: []byte("Upgrade")}, {Name: []byte("Upgrade"), Value: []byte("websocket")}},
		mode:    bodyModeNone,
		meta:    messageMeta{kind: REQUEST},
	})
	if !p.Upgrade {
		t.Fatal("Upgrade = false, want true")
	}
	if p.contentLength != unsetContentLength {
		t.Fatalf("contentLength = %d, want unset", p.contentLength)
	}

	p = New(BOTH)
	p.applyParsedMessage(parsedMessage{
		kind:    REQUEST,
		request: requestLine{Method: CONNECT, Major: 1, Minor: 1},
		mode:    bodyModeContentLength,
		meta:    messageMeta{kind: REQUEST, hasContentLength: true, contentLength: 5},
	})
	if !p.Upgrade {
		t.Fatal("Upgrade = false for CONNECT, want true")
	}
	if p.contentLength != 5 {
		t.Fatalf("contentLength = %d, want 5", p.contentLength)
	}
}

func TestEmitParsedMessageEmitsRemainingFragmentsAndCompletes(t *testing.T) {
	p := New(REQUEST)
	p.pending = []byte("GET /hello HTTP/1.1\r\nX-Test: value\r\n\r\nbody")
	p.urlOffset = len("/he")
	p.headerValueOffset = len("va")
	var events []string
	setting := &Setting{
		URL:             func(_ *Parser, b []byte, _ int) { events = append(events, "url:"+string(b)) },
		HeaderField:     func(_ *Parser, b []byte, _ int) { events = append(events, "field:"+string(b)) },
		HeaderValue:     func(_ *Parser, b []byte, _ int) { events = append(events, "value:"+string(b)) },
		HeadersComplete: func(*Parser, int) { events = append(events, "headers") },
		Body:            func(_ *Parser, b []byte, _ int) { events = append(events, "body:"+string(b)) },
		MessageComplete: func(*Parser, int) { events = append(events, "done") },
	}

	p.emitParsedMessage(setting, parsedMessage{
		kind:    REQUEST,
		request: requestLine{Target: []byte("/hello")},
		headers: []headerLine{{Name: []byte("X-Test"), Value: []byte("value")}},
		body:    []byte("body"),
	})

	want := []string{"url:llo", "field:X-Test", "value:lue", "headers", "body:body", "done"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestEmitPartialFragmentsHelpers(t *testing.T) {
	t.Run("request URL fragment", func(t *testing.T) {
		p := New(REQUEST)
		p.pending = []byte("GET /hel")
		var got []byte
		p.emitPartialRequestFragments(&Setting{URL: func(_ *Parser, b []byte, _ int) {
			got = append(got[:0], b...)
		}})
		if string(got) != "/hel" {
			t.Fatalf("URL fragment = %q, want %q", string(got), "/hel")
		}
	})

	t.Run("response status fragment", func(t *testing.T) {
		p := New(RESPONSE)
		p.pending = []byte("HTTP/1.1 200 Parti")
		var got []byte
		p.emitPartialResponseFragments(&Setting{Status: func(_ *Parser, b []byte, _ int) {
			got = append(got[:0], b...)
		}})
		if string(got) != "Parti" {
			t.Fatalf("status fragment = %q, want %q", string(got), "Parti")
		}
	})

	t.Run("header value fragment", func(t *testing.T) {
		p := New(REQUEST)
		p.pending = []byte("GET / HTTP/1.1\r\nHost: exa")
		var got []byte
		p.emitPartialHeaderValue(&Setting{HeaderValue: func(_ *Parser, b []byte, _ int) {
			got = append(got[:0], b...)
		}}, p.pending)
		if string(got) != "exa" {
			t.Fatalf("header value fragment = %q, want %q", string(got), "exa")
		}
	})
}

func TestExecuteCleanHandlesEmptyAndPartialInput(t *testing.T) {
	t.Run("empty buffer returns zero nil", func(t *testing.T) {
		p := New(REQUEST)
		consumed, err := p.executeClean(&Setting{}, nil)
		if err != nil {
			t.Fatalf("executeClean(nil) error = %v", err)
		}
		if consumed != 0 {
			t.Fatalf("consumed = %d, want 0", consumed)
		}
	})

	t.Run("partial request emits URL fragment and reports consumed", func(t *testing.T) {
		p := New(REQUEST)
		var fragments []string
		setting := &Setting{URL: func(_ *Parser, b []byte, _ int) {
			fragments = append(fragments, string(b))
		}}
		input := []byte("GET /hel")
		consumed, err := p.executeClean(setting, input)
		if err != nil {
			t.Fatalf("executeClean(partial request) error = %v", err)
		}
		if consumed != len(input) {
			t.Fatalf("consumed = %d, want %d", consumed, len(input))
		}
		if !reflect.DeepEqual(fragments, []string{"/hel"}) {
			t.Fatalf("fragments = %v, want [/hel]", fragments)
		}
	})

	t.Run("partial header value emits trailing fragment", func(t *testing.T) {
		p := New(REQUEST)
		var got []string
		setting := &Setting{HeaderValue: func(_ *Parser, b []byte, _ int) {
			got = append(got, string(b))
		}}
		input := []byte("GET / HTTP/1.1\r\nHost: exa")
		consumed, err := p.executeClean(setting, input)
		if err != nil {
			t.Fatalf("executeClean(partial header) error = %v", err)
		}
		if consumed != len("GET / HTTP/1.1\r\nHost: ") {
			t.Fatalf("consumed = %d", consumed)
		}
		if !reflect.DeepEqual(got, []string{"exa"}) {
			t.Fatalf("header fragments = %v, want [exa]", got)
		}
	})
}

func TestEmitParsedMessageResponse(t *testing.T) {
	p := New(RESPONSE)
	p.pending = []byte("HTTP/1.1 200 Partial\r\nX-Test: value\r\n\r\n")
	p.statusOffset = len("Par")
	var events []string
	setting := &Setting{
		Status:          func(_ *Parser, b []byte, _ int) { events = append(events, "status:"+string(b)) },
		HeaderField:     func(_ *Parser, b []byte, _ int) { events = append(events, "field:"+string(b)) },
		HeaderValue:     func(_ *Parser, b []byte, _ int) { events = append(events, "value:"+string(b)) },
		HeadersComplete: func(*Parser, int) { events = append(events, "headers") },
		MessageComplete: func(*Parser, int) { events = append(events, "done") },
	}

	p.emitParsedMessage(setting, parsedMessage{
		kind:     RESPONSE,
		response: responseLine{Reason: []byte("Partial")},
		headers:  []headerLine{{Name: []byte("X-Test"), Value: []byte("value")}},
	})

	want := []string{"status:tial", "field:X-Test", "value:value", "headers", "done"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestExecuteCleanReturnsSemanticHeaderErrors(t *testing.T) {
	p := New(REQUEST)
	_, err := p.executeClean(&Setting{}, []byte("GET / HTTP/1.1\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\na"))
	if err == nil {
		t.Fatal("executeClean() error = nil, want duplicate Content-Length error")
	}
}

func TestApplyParsedMessageResponseUpgradeSwitchesState(t *testing.T) {
	p := New(BOTH)
	p.applyParsedMessage(parsedMessage{
		kind:     RESPONSE,
		response: responseLine{Major: 1, Minor: 1, StatusCode: 101},
		headers:  []headerLine{{Name: []byte("Connection"), Value: []byte("Upgrade")}, {Name: []byte("Upgrade"), Value: []byte("websocket")}},
		mode:     bodyModeNone,
		meta:     messageMeta{kind: RESPONSE},
	})
	if p.Method != 0 {
		t.Fatalf("Method = %v, want 0 for response", p.Method)
	}
	if !p.Upgrade {
		t.Fatal("Upgrade = false, want true for 101 response upgrade")
	}
}

func TestSmallEngineHelpers(t *testing.T) {
	t.Run("hasNoCallbacks", func(t *testing.T) {
		if !hasNoCallbacks(nil) {
			t.Fatal("hasNoCallbacks(nil) = false, want true")
		}
		if !hasNoCallbacks(&Setting{}) {
			t.Fatal("hasNoCallbacks(empty) = false, want true")
		}
		if hasNoCallbacks(&Setting{URL: func(*Parser, []byte, int) {}}) {
			t.Fatal("hasNoCallbacks(URL callback) = true, want false")
		}
	})

	t.Run("inferMessageKind", func(t *testing.T) {
		if got, ok := inferMessageKind(BOTH, []byte("HTTP/1.1 200 OK")); !ok || got != RESPONSE {
			t.Fatalf("inferMessageKind(response) = (%v, %t)", got, ok)
		}
		if got, ok := inferMessageKind(BOTH, []byte("GET / HTTP/1.1")); !ok || got != REQUEST {
			t.Fatalf("inferMessageKind(request) = (%v, %t)", got, ok)
		}
		if _, ok := inferMessageKind(BOTH, []byte("H")); ok {
			t.Fatal("inferMessageKind(short H) ok = true, want false")
		}
	})

	t.Run("scanCRLFBytes", func(t *testing.T) {
		if idx, ok := scanCRLFBytes([]byte("abc\r\ndef")); !ok || idx != 3 {
			t.Fatalf("scanCRLFBytes() = (%d, %t), want (3, true)", idx, ok)
		}
		if _, ok := scanCRLFBytes([]byte("abc\ndef")); ok {
			t.Fatal("scanCRLFBytes(LF only) ok = true, want false")
		}
	})

	t.Run("applyHeaderMeta", func(t *testing.T) {
		meta := messageMeta{}
		if !applyHeaderMeta(&meta, headerLine{Name: []byte("Content-Length"), Value: []byte("12")}) {
			t.Fatal("applyHeaderMeta(content-length) = false, want true")
		}
		if !meta.hasContentLength || meta.contentLength != 12 {
			t.Fatalf("meta after content-length = %#v", meta)
		}
		if applyHeaderMeta(&meta, headerLine{Name: []byte("Content-Length"), Value: []byte("13")}) {
			t.Fatal("applyHeaderMeta(duplicate content-length) = true, want false")
		}
		meta = messageMeta{}
		if !applyHeaderMeta(&meta, headerLine{Name: []byte("Transfer-Encoding"), Value: []byte("chunked")}) {
			t.Fatal("applyHeaderMeta(chunked) = false, want true")
		}
		if !meta.hasTransferEncoding {
			t.Fatal("hasTransferEncoding = false, want true")
		}
	})
}

func TestParseMessageDetailedResponseAndTrailingBytes(t *testing.T) {
	msg, result := parseMessageDetailed([]byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n"), RESPONSE)
	if result.err != nil || !result.ok {
		t.Fatalf("parseMessageDetailed(chunked response) = (%#v, %#v)", msg, result)
	}
	if msg.response.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", msg.response.StatusCode)
	}
	if string(msg.body) != "hello" {
		t.Fatalf("body = %q, want %q", string(msg.body), "hello")
	}

	_, result = parseMessageDetailed([]byte("HTTP/1.1 200 OK\r\nContent-Length: 1\r\n\r\naEXTRA"), RESPONSE)
	if result.ok || result.err != nil {
		t.Fatalf("parseMessageDetailed(trailing bytes) = %#v, want incomplete result", result)
	}
}

func TestParseMessageDetailedReportsHeaderSemanticError(t *testing.T) {
	_, result := parseMessageDetailed([]byte("GET / HTTP/1.1\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\na"), REQUEST)
	if result.err == nil {
		t.Fatal("parseMessageDetailed() error = nil, want duplicate Content-Length error")
	}
}

func TestExecuteCleanResponsePartialStatusAndSemanticError(t *testing.T) {
	t.Run("partial response emits status fragment", func(t *testing.T) {
		p := New(RESPONSE)
		var got []string
		setting := &Setting{Status: func(_ *Parser, b []byte, _ int) {
			got = append(got, string(b))
		}}
		input := []byte("HTTP/1.1 200 Part")
		consumed, err := p.executeClean(setting, input)
		if err != nil {
			t.Fatalf("executeClean(partial response) error = %v", err)
		}
		if consumed != len(input) {
			t.Fatalf("consumed = %d, want %d", consumed, len(input))
		}
		if !reflect.DeepEqual(got, []string{"Part"}) {
			t.Fatalf("status fragments = %v, want [Part]", got)
		}
	})

	t.Run("semantic header error bubbles out", func(t *testing.T) {
		p := New(REQUEST)
		_, err := p.executeClean(&Setting{}, []byte("GET / HTTP/1.1\r\nContent-Length: nope\r\n\r\n"))
		if err == nil {
			t.Fatal("executeClean() error = nil, want semantic error")
		}
	})
}
