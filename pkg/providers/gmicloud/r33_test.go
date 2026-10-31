package gmicloud

import "testing"

// message.trim() is JS trim: U+0085 is not whitespace there, U+FEFF is.
func TestR33_DetailsMessageUsesJSTrim(t *testing.T) {
	if got := unwrapGmicloudDetailsMessage("{\"error\":{\"message\":\"\\u0085\"}}"); got != "\u0085" {
		t.Errorf("NEL message = %q", got)
	}
	if got := unwrapGmicloudDetailsMessage("{\"error\":{\"message\":\"\\ufeff \"}}"); got != "" {
		t.Errorf("BOM message = %q", got)
	}
}
