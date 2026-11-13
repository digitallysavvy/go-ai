package openai

import "testing"

// TS escapeJSONDelta is JSON.stringify(delta).slice(1, -1): <, > and & stay
// literal and U+2028 is written as is.
func TestEscapeJSONDeltaMatchesJSONStringify(t *testing.T) {
	cases := map[string]string{
		"a && b < c > d":     "a && b < c > d",
		"line\n\"quoted\"\\": `line\n\"quoted\"\\`,
		"sep end":            "sep end",
		"<div class=\"x\"/>": `<div class=\"x\"/>`,
	}
	for in, want := range cases {
		if got := escapeJSONDelta(in); got != want {
			t.Errorf("escapeJSONDelta(%q) = %q, want %q", in, got, want)
		}
	}
}
