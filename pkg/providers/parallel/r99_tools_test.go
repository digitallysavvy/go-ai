package parallel

import (
	"context"
	"math"
	"testing"
)

// usage.input_tokens is any non-negative safe integer (TS
// z.number().int().nonnegative()). int(f) overflowed for a count above
// 2^31-1 on 32-bit targets; the count is now clamped to the platform int.
func TestR99toolsParallelLargeInputTokens(t *testing.T) {
	fixture := r85toolsFixture(t)
	fixture["usage"] = map[string]interface{}{"input_tokens": 3000000000.0, "output_tokens": 0.0}
	_, m, _ := r85toolsSetup(t, fixture, 200)
	res, err := m.DoDecide(context.Background(), r85toolsOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := int64(3000000000)
	if want > math.MaxInt {
		want = math.MaxInt
	}
	if int64(*res.Usage.InputTokens) != want {
		t.Fatalf("input tokens = %d, want %d", *res.Usage.InputTokens, want)
	}
}
