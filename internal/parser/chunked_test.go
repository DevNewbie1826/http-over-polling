package parser

import "testing"

func TestParseChunkSizeLine(t *testing.T) {
	c := newCursor([]byte("5\r\nhello"))
	size, ok := parseChunkSizeLine(&c)
	if !ok {
		t.Fatal("parseChunkSizeLine returned ok=false")
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
	if got := c.remaining(); got != len("hello") {
		t.Fatalf("remaining = %d, want %d", got, len("hello"))
	}
}

func TestParseChunkSizeLineWithExtension(t *testing.T) {
	c := newCursor([]byte("A;foo=bar\r\nrest"))
	size, ok := parseChunkSizeLine(&c)
	if !ok {
		t.Fatal("parseChunkSizeLine returned ok=false")
	}
	if size != 10 {
		t.Fatalf("size = %d, want 10", size)
	}
}

func TestParseChunkSizeLineRejectsMalformedDelimiter(t *testing.T) {
	c := newCursor([]byte("5\nhello"))
	if _, ok := parseChunkSizeLine(&c); ok {
		t.Fatal("expected malformed chunk-size line to fail")
	}
}

func TestConsumeChunkData(t *testing.T) {
	c := newCursor([]byte("hello\r\nrest"))
	data, ok := consumeChunkData(&c, 5)
	if !ok {
		t.Fatal("consumeChunkData returned ok=false")
	}
	if string(data) != "hello" {
		t.Fatalf("data = %q, want %q", string(data), "hello")
	}
	if got := c.remaining(); got != len("rest") {
		t.Fatalf("remaining = %d, want %d", got, len("rest"))
	}
}

func TestConsumeChunkDataRejectsMissingCRLF(t *testing.T) {
	c := newCursor([]byte("hello\nrest"))
	if _, ok := consumeChunkData(&c, 5); ok {
		t.Fatal("expected malformed chunk-data delimiter to fail")
	}
}
