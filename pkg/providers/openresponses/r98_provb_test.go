package openresponses

import (
	"encoding/json"
	"math"
	"testing"
)

// TestR98provbAsIntPlatformInt: a replayed annotation index read as an
// int64 or json.Number is clamped to the platform int like the float64 case
// (a plain conversion wraps on 32-bit targets).
func TestR98provbAsIntPlatformInt(t *testing.T) {
	want := int64(math.MaxInt)
	if want > 3_000_000_000 {
		want = 3_000_000_000
	}
	for _, in := range []interface{}{int64(3_000_000_000), json.Number("3000000000"), float64(3_000_000_000)} {
		got, ok := asInt(in)
		if !ok || int64(got) != want {
			t.Errorf("%T: %d, want %d", in, got, want)
		}
	}
	if got, _ := asInt(int64(-3_000_000_000)); got > 0 {
		t.Errorf("negative int64: %d, want a negative value", got)
	}
}
