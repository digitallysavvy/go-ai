package middleware

import "testing"

// JS \s and trim() cover \v, NBSP and U+FEFF but not U+0085; Go's RE2 \s and
// strings.TrimSpace differ on all four.
func TestDefaultJSONTransformUsesJSWhitespace(t *testing.T) {
	got := defaultJSONTransform("```json\v\n{\"a\":1}\n```\u00a0\ufeff")
	if got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
	if got := defaultJSONTransform("\u0085{\"a\":1}"); got != "\u0085{\"a\":1}" {
		t.Fatalf("U+0085 is not JS whitespace, got %q", got)
	}
}

func TestStripSuffixAndPotentialSuffixJSWhitespace(t *testing.T) {
	if got := stripMarkdownCodeFenceSuffix("x\n```\u00a0"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := getPotentialSuffixStart("ab\u00a0`"); got != 2 {
		t.Fatalf("got %d", got)
	}
	if got := getPotentialSuffixStart("ab\u0085"); got != len("ab\u0085") {
		t.Fatalf("U+0085 must not count as whitespace, got %d", got)
	}
}
