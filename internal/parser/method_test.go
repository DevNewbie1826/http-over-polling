package parser

import "testing"

func TestMethodString(t *testing.T) {
	tests := []struct {
		method Method
		want   string
	}{
		{method: GET, want: "GET"},
		{method: HEAD, want: "HEAD"},
		{method: MSEARCH, want: "M-SEARCH"},
		{method: Method(0), want: "UNKNOWN"},
		{method: Method(127), want: "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.method.String(); got != tt.want {
			t.Fatalf("Method(%d).String() = %q, want %q", tt.method, got, tt.want)
		}
	}
}
