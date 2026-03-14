package benchparity

import "testing"

func TestHTTPResponseBodyBytesMatchesString(t *testing.T) {
	if string(HTTPResponseBodyBytes) != HTTPResponseBody {
		t.Fatalf("HTTPResponseBodyBytes = %q, want %q", string(HTTPResponseBodyBytes), HTTPResponseBody)
	}
}
