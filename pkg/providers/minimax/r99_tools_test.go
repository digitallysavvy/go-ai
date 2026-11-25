package minimax

import (
	"math"
	"testing"
)

// MiniMax image usage counts are any JSON number (z.number().nullish()).
// int(f) of a value beyond the platform int is implementation-defined (it
// saturates on arm64, wraps on amd64 and overflows on 32-bit targets); the
// count is now clamped.
func TestR99toolsTokenCountClampsToPlatformInt(t *testing.T) {
	big, neg, mid := 1e20, -1e20, 3e9
	if got := tokenCount(&big); got != math.MaxInt {
		t.Errorf("1e20 = %d", got)
	}
	if got := tokenCount(&neg); got != math.MinInt {
		t.Errorf("-1e20 = %d", got)
	}
	want := int64(3000000000)
	if want > math.MaxInt {
		want = math.MaxInt
	}
	if got := tokenCount(&mid); int64(got) != want {
		t.Errorf("3e9 = %d, want %d", got, want)
	}
}
