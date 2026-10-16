package providerutils

import (
	"math"
	"testing"
)

func TestJSNumber(t *testing.T) {
	for in, want := range map[string]float64{"": 0, " 44100 ": 44100, "4.41e4": 44100, "0xac44": 44100, "0b11": 3, ".5": 0.5, "-Infinity": math.Inf(-1)} {
		if got, ok := JSNumber(in); !ok || got != want {
			t.Errorf("JSNumber(%q) = %v, %v", in, got, ok)
		}
	}
	for _, in := range []string{"abc", "inf", "nan", "1_0", "0x1p3", "1e", "--1", "12px", "0x"} {
		if _, ok := JSNumber(in); ok {
			t.Errorf("JSNumber(%q) accepted", in)
		}
	}
	if got, ok := JSNumber("1e999"); !ok || !math.IsInf(got, 1) {
		t.Errorf("1e999 = %v, %v", got, ok)
	}
}
