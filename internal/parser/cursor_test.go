package parser

import "testing"

func TestCursorAdvanceAndRemaining(t *testing.T) {
	c := newCursor([]byte("hello"))
	if got := c.remaining(); got != 5 {
		t.Fatalf("remaining = %d, want 5", got)
	}
	if b, ok := c.peek(); !ok || b != 'h' {
		t.Fatalf("peek = (%q, %v), want ('h', true)", b, ok)
	}

	c.advance(2)

	if got := c.remaining(); got != 3 {
		t.Fatalf("remaining = %d, want 3", got)
	}
	if b, ok := c.peek(); !ok || b != 'l' {
		t.Fatalf("peek = (%q, %v), want ('l', true)", b, ok)
	}
}

func TestCursorScanByte(t *testing.T) {
	c := newCursor([]byte("Host: example.com"))
	idx, ok := c.scanByte(':')
	if !ok || idx != 4 {
		t.Fatalf("scanByte = (%d, %v), want (4, true)", idx, ok)
	}
}

func TestCursorSliceFromCurrent(t *testing.T) {
	c := newCursor([]byte("abcdef"))
	c.advance(2)
	if got := string(c.slice(2)); got != "cd" {
		t.Fatalf("slice = %q, want %q", got, "cd")
	}
}
