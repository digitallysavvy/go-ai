package zodlite

import (
	"math"
	"testing"
)

// cr-r95-carry-to-integrator (optional part): zod 4.6.7 under node rejects a
// non-finite number in z.number() / .int() / .min() / .max() with
// invalid_type, and names it by its value in every received text.
func TestR95integNonFiniteNumbers(t *testing.T) {
	for _, c := range []struct {
		v    float64
		name string
	}{{math.Inf(1), "Infinity"}, {math.Inf(-1), "-Infinity"}, {math.NaN(), "NaN"}} {
		for label, n := range map[string]Node{"number": Number, "int": Int, "min": Min(Number, 0), "max": Max(Number, 10)} {
			bad := n(c.v, true, nil)
			if bad == nil || bad.Code != "invalid_type" || bad.Message != "Invalid input: expected number, received "+c.name {
				t.Errorf("%s(%v) = %+v, want invalid_type received %s", label, c.v, bad, c.name)
			}
		}
		if bad := String(c.v, true, nil); bad == nil || bad.Message != "Invalid input: expected string, received "+c.name {
			t.Errorf("String(%v) = %+v", c.v, bad)
		}
	}
	if bad := Number(1.5, true, nil); bad != nil {
		t.Errorf("Number(1.5) = %+v", bad)
	}
}
