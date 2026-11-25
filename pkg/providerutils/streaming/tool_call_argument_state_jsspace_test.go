package streaming

import "testing"

func TestStartsWithStructuredValueJSWhitespace(t *testing.T) {
	if !StartsWithStructuredValue("\ufeff{") {
		t.Error("U+FEFF is JS whitespace (trimStart)")
	}
	if StartsWithStructuredValue("\u0085{") {
		t.Error("U+0085 is not JS whitespace")
	}
	s := NewStreamingToolCallArgumentState("\ufeff{}")
	if !s.HasCompleteStructuredValue() {
		t.Error("expected complete structured value after BOM")
	}
}
