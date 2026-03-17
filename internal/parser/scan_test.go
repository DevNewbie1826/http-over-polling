package parser

import "testing"

func TestParseDecimalInt32(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int32
		ok    bool
	}{
		{name: "simple", input: []byte("12345"), want: 12345, ok: true},
		{name: "zero", input: []byte("0"), want: 0, ok: true},
		{name: "empty", input: nil, want: 0, ok: false},
		{name: "non-digit", input: []byte("12x"), want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseDecimalInt32(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseDecimalInt32(%q) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestEqualFoldToken(t *testing.T) {
	if !equalFoldToken([]byte("Content-Length"), "content-length") {
		t.Fatal("expected token match")
	}

	if equalFoldToken([]byte("Content-Length"), "transfer-encoding") {
		t.Fatal("expected token mismatch")
	}
}

func TestMatchMethod(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  Method
		ok    bool
	}{
		{name: "get", input: []byte("GET"), want: GET, ok: true},
		{name: "head", input: []byte("HEAD"), want: HEAD, ok: true},
		{name: "m-search", input: []byte("M-SEARCH"), want: MSEARCH, ok: true},
		{name: "lowercase", input: []byte("post"), want: POST, ok: true},
		{name: "unknown", input: []byte("BREW"), want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := matchMethod(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("matchMethod(%q) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestScanToByte(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		target byte
		want   int
		ok     bool
	}{
		{name: "finds colon", input: []byte("Host: example.com"), target: ':', want: 4, ok: true},
		{name: "finds space", input: []byte("GET / HTTP/1.1"), target: ' ', want: 3, ok: true},
		{name: "missing", input: []byte("abcdef"), target: ':', want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := scanToByte(tt.input, tt.target)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("scanToByte(%q, %q) = (%d, %v), want (%d, %v)", tt.input, tt.target, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSkipSpaces(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  int
	}{
		{name: "none", input: []byte("abc"), want: 0},
		{name: "spaces", input: []byte("   abc"), want: 3},
		{name: "tabs and spaces", input: []byte(" \t\tabc"), want: 3},
		{name: "all whitespace", input: []byte(" \t "), want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := skipSpaces(tt.input); got != tt.want {
				t.Fatalf("skipSpaces(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseHTTPVersionDigits(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		wantMajor   uint8
		wantMinor   uint8
		wantMatched bool
	}{
		{name: "http11", input: []byte("1.1"), wantMajor: 1, wantMinor: 1, wantMatched: true},
		{name: "http10", input: []byte("1.0"), wantMajor: 1, wantMinor: 0, wantMatched: true},
		{name: "short", input: []byte("1."), wantMatched: false},
		{name: "nondigit", input: []byte("a.1"), wantMatched: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, ok := parseHTTPVersionDigits(tt.input)
			if major != tt.wantMajor || minor != tt.wantMinor || ok != tt.wantMatched {
				t.Fatalf("parseHTTPVersionDigits(%q) = (%d, %d, %v), want (%d, %d, %v)", tt.input, major, minor, ok, tt.wantMajor, tt.wantMinor, tt.wantMatched)
			}
		})
	}
}

func TestResponseStatusHasNoBody(t *testing.T) {
	tests := []struct {
		status uint16
		want   bool
	}{
		{status: 100, want: true},
		{status: 101, want: true},
		{status: 204, want: true},
		{status: 304, want: true},
		{status: 200, want: false},
		{status: 404, want: false},
	}

	for _, tt := range tests {
		if got := responseStatusHasNoBody(tt.status); got != tt.want {
			t.Fatalf("responseStatusHasNoBody(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
