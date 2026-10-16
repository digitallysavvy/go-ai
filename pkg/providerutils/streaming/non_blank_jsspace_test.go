package streaming

import "testing"

func TestNonBlankUsesJSWhitespace(t *testing.T) {
	if got := nonBlank("\ufeff\u00a0"); got != "" {
		t.Errorf("BOM/NBSP are JS whitespace, got %q", got)
	}
	if got := nonBlank("\u0085"); got == "" {
		t.Error("U+0085 is not JS whitespace")
	}
}
