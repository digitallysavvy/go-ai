package providerutils

import (
	"math"
	"testing"
)

// TestR99provbWireIntClamps: a wire number beyond the platform int is
// clamped, not an implementation-defined conversion (MinInt64 on amd64,
// wrapped on 32-bit targets).
func TestR99provbWireIntClamps(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{{1e19, math.MaxInt}, {-1e19, math.MinInt}, {12.9, 12}, {-3.5, -3}, {math.NaN(), 0}} {
		if got := WireInt(tc.in); float64(got) != tc.want {
			t.Errorf("WireInt(%v) = %d, want %v", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in   float64
		want int64
	}{{1e19, math.MaxInt64}, {-1e19, math.MinInt64}, {42.7, 42}} {
		if got := WireInt64(tc.in); got != tc.want {
			t.Errorf("WireInt64(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
