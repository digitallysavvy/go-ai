package fal

import "testing"

// r91provcWhole reads falNumber's int / int64 result back as a float.
func r91provcWhole(t *testing.T, v interface{}) float64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case *int:
		return float64(*n)
	case *int64:
		return float64(*n)
	}
	t.Fatalf("unexpected %T", v)
	return 0
}

// TestR91provcFalNumberKeepsLargeSeeds (provc-03): a fal seed above 2^31-1
// keeps its value on every platform (TS passes the JS number through,
// fal-video-model.ts:357); an in-range value stays an int.
func TestR91provcFalNumberKeepsLargeSeeds(t *testing.T) {
	for _, f := range []float64{3851223452, -3851223452, 42} {
		if got := r91provcWhole(t, falNumber(f)); got != f {
			t.Errorf("falNumber(%v) = %v", f, got)
		}
		ff := f
		if got := r91provcWhole(t, falNumberPtr(&ff)); got != f {
			t.Errorf("falNumberPtr(%v) = %v", f, got)
		}
	}
	if _, ok := falNumber(42).(int); !ok {
		t.Error("an in-range value must stay an int (v0.5.2 type)")
	}
}
