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

func TestJSNumberString(t *testing.T) {
	for f, want := range map[float64]string{
		0: "0", 1: "1", -1.5: "-1.5", 100: "100", 1e20: "100000000000000000000", 1e21: "1e+21", 1.5e21: "1.5e+21",
		0.000001: "0.000001", 1e-7: "1e-7", 1.5e-7: "1.5e-7", 123456789012345680000: "123456789012345680000",
		0.1: "0.1", 22050.5: "22050.5", 5e-324: "5e-324", math.MaxFloat64: "1.7976931348623157e+308",
	} {
		if got := JSNumberString(f); got != want {
			t.Errorf("JSNumberString(%v) = %q, want %q", f, got, want)
		}
	}
	if JSNumberString(math.NaN()) != "NaN" || JSNumberString(math.Inf(-1)) != "-Infinity" {
		t.Error("non-finite")
	}
}

// JS Number("0x...") is not limited to 64 bits: it rounds the exact value.
func TestJSNumberHexBeyondUint64(t *testing.T) {
	got, ok := JSNumber("0xFFFFFFFFFFFFFFFFF")
	if !ok || got != 295147905179352825856 {
		t.Fatalf("JSNumber = %v, %v, want 2.9514790517935283e+20", got, ok)
	}
	if got, ok := JSNumber("0b" + string(make([]byte, 0)) + "1111111111111111111111111111111111111111111111111111111111111111111"); !ok || got != math.Pow(2, 67) {
		t.Fatalf("binary beyond 64 bits = %v, %v", got, ok)
	}
}
