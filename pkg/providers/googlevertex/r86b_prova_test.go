package googlevertex

import "testing"

// TS Number.parseFloat skips leading JS whitespace (U+FEFF, U+00A0, \v), not
// U+0085. Node: 1.5, 2, 5, NaN.
func TestR86bprovaParseGoogleDurationJSWhitespace(t *testing.T) {
	for in, want := range map[string]float64{"\ufeff1.5s": 1.5, "\u00a02s": 2, "\v.5e1s": 5, "\u00851s": -1} {
		got, ok := parseGoogleDuration(in)
		if want < 0 {
			if ok {
				t.Fatalf("%q: got %v, want NaN", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("%q: got %v %v, want %v", in, got, ok, want)
		}
	}
}
