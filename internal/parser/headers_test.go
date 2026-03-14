package parser

import "testing"

func TestParseHeaderLine(t *testing.T) {
	c := newCursor([]byte("Host: example.com\r\n"))
	h, ok := parseHeaderLine(&c)
	if !ok {
		t.Fatal("parseHeaderLine returned ok=false")
	}
	if string(h.Name) != "Host" {
		t.Fatalf("name = %q, want %q", string(h.Name), "Host")
	}
	if string(h.Value) != "example.com" {
		t.Fatalf("value = %q, want %q", string(h.Value), "example.com")
	}
}

func TestParseHeaderLineTrimsLeadingOWSInValue(t *testing.T) {
	c := newCursor([]byte("Connection:   Upgrade\r\n"))
	h, ok := parseHeaderLine(&c)
	if !ok {
		t.Fatal("parseHeaderLine returned ok=false")
	}
	if string(h.Value) != "Upgrade" {
		t.Fatalf("value = %q, want %q", string(h.Value), "Upgrade")
	}
}

func TestParseHeaderLineNeedsMoreData(t *testing.T) {
	c := newCursor([]byte("Host: exa"))
	if _, ok := parseHeaderLine(&c); ok {
		t.Fatal("expected partial header line to need more data")
	}
}

func TestParseHeaderLineRejectsMissingColon(t *testing.T) {
	c := newCursor([]byte("Host example.com\r\n"))
	if _, ok := parseHeaderLine(&c); ok {
		t.Fatal("expected malformed header line to fail")
	}
}
