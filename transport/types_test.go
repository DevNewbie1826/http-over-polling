package transport

import (
	"testing"
	"time"
)

func TestOptionsSetExpectedTimeouts(t *testing.T) {
	var opts options
	WithReadTimeout(3 * time.Second)(&opts)
	WithWriteTimeout(5 * time.Second)(&opts)
	WithIdleTimeout(7 * time.Second)(&opts)

	if opts.readTimeout != 3*time.Second {
		t.Fatalf("readTimeout = %s, want %s", opts.readTimeout, 3*time.Second)
	}
	if opts.writeTimeout != 5*time.Second {
		t.Fatalf("writeTimeout = %s, want %s", opts.writeTimeout, 5*time.Second)
	}
	if opts.idleTimeout != 7*time.Second {
		t.Fatalf("idleTimeout = %s, want %s", opts.idleTimeout, 7*time.Second)
	}
}

func TestOptionsOverrideExistingTimeoutValue(t *testing.T) {
	var opts options
	WithReadTimeout(time.Second)(&opts)
	WithReadTimeout(2 * time.Second)(&opts)

	if opts.readTimeout != 2*time.Second {
		t.Fatalf("readTimeout = %s, want %s", opts.readTimeout, 2*time.Second)
	}
}
