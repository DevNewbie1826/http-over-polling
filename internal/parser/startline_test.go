package parser

import "testing"

func TestParseRequestLine(t *testing.T) {
	c := newCursor([]byte("GET /hello HTTP/1.1\r\n"))
	line, ok := parseRequestLine(&c)
	if !ok {
		t.Fatal("parseRequestLine returned ok=false")
	}
	if line.Method != GET {
		t.Fatalf("method = %v, want %v", line.Method, GET)
	}
	if string(line.Target) != "/hello" {
		t.Fatalf("target = %q, want %q", string(line.Target), "/hello")
	}
	if line.Major != 1 || line.Minor != 1 {
		t.Fatalf("version = %d.%d, want 1.1", line.Major, line.Minor)
	}
}

func TestParseResponseLine(t *testing.T) {
	c := newCursor([]byte("HTTP/1.1 204 No Content\r\n"))
	line, ok := parseResponseLine(&c)
	if !ok {
		t.Fatal("parseResponseLine returned ok=false")
	}
	if line.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", line.StatusCode)
	}
	if string(line.Reason) != "No Content" {
		t.Fatalf("reason = %q, want %q", string(line.Reason), "No Content")
	}
	if line.Major != 1 || line.Minor != 1 {
		t.Fatalf("version = %d.%d, want 1.1", line.Major, line.Minor)
	}
}

func TestParseRequestLineNeedsMoreData(t *testing.T) {
	c := newCursor([]byte("GET /hel"))
	if _, ok := parseRequestLine(&c); ok {
		t.Fatal("expected partial request line to need more data")
	}
}

func TestParseResponseLineRejectsMalformedVersion(t *testing.T) {
	c := newCursor([]byte("HTTP/x.y 200 OK\r\n"))
	if _, ok := parseResponseLine(&c); ok {
		t.Fatal("expected malformed response line to fail")
	}
}
