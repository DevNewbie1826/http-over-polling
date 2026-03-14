package parser

import "testing"

func TestParseMessageRequestNoBody(t *testing.T) {
	result, ok := parseMessage([]byte("GET /hello HTTP/1.1\r\nHost: example.com\r\n\r\n"), REQUEST)
	if !ok {
		t.Fatal("parseMessage returned ok=false")
	}
	if result.kind != REQUEST {
		t.Fatalf("kind = %v, want %v", result.kind, REQUEST)
	}
	if result.request.Method != GET {
		t.Fatalf("method = %v, want %v", result.request.Method, GET)
	}
	if string(result.request.Target) != "/hello" {
		t.Fatalf("target = %q, want %q", string(result.request.Target), "/hello")
	}
	if len(result.headers) != 1 || string(result.headers[0].Name) != "Host" {
		t.Fatalf("headers = %#v, want Host header", result.headers)
	}
	if result.mode != bodyModeNone {
		t.Fatalf("mode = %v, want %v", result.mode, bodyModeNone)
	}
}

func TestParseMessageResponseNoBodyStatus(t *testing.T) {
	result, ok := parseMessage([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"), RESPONSE)
	if !ok {
		t.Fatal("parseMessage returned ok=false")
	}
	if result.kind != RESPONSE {
		t.Fatalf("kind = %v, want %v", result.kind, RESPONSE)
	}
	if result.response.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", result.response.StatusCode)
	}
	if result.mode != bodyModeNone {
		t.Fatalf("mode = %v, want %v", result.mode, bodyModeNone)
	}
}

func TestParseMessageRequestContentLengthBody(t *testing.T) {
	result, ok := parseMessage([]byte("POST /upload HTTP/1.1\r\nContent-Length: 5\r\n\r\nhello"), REQUEST)
	if !ok {
		t.Fatal("parseMessage returned ok=false")
	}
	if result.mode != bodyModeContentLength {
		t.Fatalf("mode = %v, want %v", result.mode, bodyModeContentLength)
	}
	if string(result.body) != "hello" {
		t.Fatalf("body = %q, want %q", string(result.body), "hello")
	}
}

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
