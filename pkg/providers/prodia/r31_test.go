package prodia

import "testing"

// TS getArgs: both parts non-empty and Number() finite; zero, negative and
// fractional values are accepted, anything else is an invalid size.
func TestParseProdiaSizeMatchesTS(t *testing.T) {
	for in, want := range map[string]bool{
		"1024x768": true, "0x0": true, "-5x10": true, "1024.5x768": true, "1e3x2": true, " 10 x 20 ": true,
		"1024": false, "x768": false, "1024x": false, "axb": false, "10x20x30": true, "0x1Fx2": false,
	} {
		if _, _, ok := parseProdiaSize(in); ok != want {
			t.Errorf("parseProdiaSize(%q) ok = %v, want %v", in, ok, want)
		}
	}
}
