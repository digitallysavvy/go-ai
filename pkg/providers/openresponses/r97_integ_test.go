package openresponses

import (
	"math"
	"testing"
)

// TestR97integAnnotationIndexNonFiniteDeterministic: a url_citation index of
// 1e400 (+Inf under ruling 4) or NaN goes into the v0.5.2 int field as the
// nearest int on every platform; a plain Go conversion is
// implementation-defined (MinInt64 on amd64, MaxInt64 on arm64). Known
// difference "Fractional counts in integer fields" (TS keeps the double).
func TestR97integAnnotationIndexNonFiniteDeterministic(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want int
	}{{math.Inf(1), math.MaxInt}, {math.Inf(-1), math.MinInt}, {math.NaN(), 0}, {1e300, math.MaxInt}, {2.5, 2}} {
		if got, ok := asInt(c.in); !ok || got != c.want {
			t.Errorf("asInt(%v) = %d, %v; want %d", c.in, got, ok, c.want)
		}
	}
	anns := openResponsesParseAnnotations([]interface{}{map[string]interface{}{"type": "url_citation", "start_index": math.Inf(1), "end_index": 3.0, "url": "u", "title": "t"}})
	if len(anns) != 1 || anns[0].StartIndex != math.MaxInt || anns[0].EndIndex != 3 {
		t.Fatalf("annotations = %+v", anns)
	}
}
