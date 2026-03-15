//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd || rumprun || (zos && s390x)

package tcplisten

import (
	"math"
	"strconv"
	"testing"
)

func TestSafeIntToUint32ValidValues(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want uint32
	}{
		{name: "zero", in: 0, want: 0},
	}
	if strconv.IntSize >= 64 {
		tests = append(tests, struct {
			name string
			in   int
			want uint32
		}{name: "maxUint32", in: int(math.MaxUint32), want: math.MaxUint32})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeIntToUint32(tt.in)
			if err != nil {
				t.Fatalf("safeIntToUint32(%d) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("safeIntToUint32(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSafeIntToUint32RejectsOutOfRangeValues(t *testing.T) {
	got, err := safeIntToUint32(-1)
	if err == nil {
		t.Fatal("safeIntToUint32(-1) error = nil, want error")
	}
	if got != 0 {
		t.Fatalf("safeIntToUint32(-1) = %d, want 0", got)
	}

	if strconv.IntSize >= 64 {
		got, err = safeIntToUint32(int(math.MaxUint32) + 1)
		if err == nil {
			t.Fatal("safeIntToUint32(MaxUint32+1) error = nil, want error")
		}
		if got != 0 {
			t.Fatalf("safeIntToUint32(MaxUint32+1) = %d, want 0", got)
		}
	}
}
