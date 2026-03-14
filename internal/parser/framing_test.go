package parser

import "testing"

func TestDecideBodyMode(t *testing.T) {
	tests := []struct {
		name string
		meta messageMeta
		want bodyMode
	}{
		{name: "request no body", meta: messageMeta{kind: REQUEST}, want: bodyModeNone},
		{name: "content length", meta: messageMeta{kind: REQUEST, hasContentLength: true, contentLength: 5}, want: bodyModeContentLength},
		{name: "zero content length", meta: messageMeta{kind: REQUEST, hasContentLength: true, contentLength: 0}, want: bodyModeNone},
		{name: "chunked wins", meta: messageMeta{kind: REQUEST, hasContentLength: true, contentLength: 5, hasTransferEncoding: true}, want: bodyModeChunked},
		{name: "response eof body", meta: messageMeta{kind: RESPONSE, statusCode: 200}, want: bodyModeEOF},
		{name: "response no body status", meta: messageMeta{kind: RESPONSE, statusCode: 204}, want: bodyModeNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideBodyMode(tt.meta); got != tt.want {
				t.Fatalf("decideBodyMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
