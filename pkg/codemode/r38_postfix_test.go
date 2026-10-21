package codemode

import "testing"

// Node reads `/` after a postfix ++/-- as division; the stripper used to start
// a regex literal and swallow the rest of the line.
func TestStripPostfixIncrementDivision(t *testing.T) {
	cases := map[string]string{
		"x = a++ / 2; let z: number = 1":               "x = a++ / 2; let z         = 1",
		"x = a-- /* c */ / 2; let z: number = 1":       "x = a-- /* c */ / 2; let z         = 1",
		"x = a[0]++ / 2 / 3; let z: number = 1":        "x = a[0]++ / 2 / 3; let z         = 1",
		"x = a\n++\n/re/.lastIndex; let z: number = 1": "x = a\n++\n/re/.lastIndex; let z         = 1",
	}
	for in, want := range cases {
		got, err := stripTypeScriptAnnotations(in)
		if err != nil || got != want {
			t.Errorf("strip(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
