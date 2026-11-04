package gemini

import "testing"

// toLowerCase class sweep (round 86b): TS google-model-catalog lowercases
// the model ID with toLowerCase, which maps U+0130 to "i\u0307" (Go's
// strings.ToLower gives "i").
func TestR86bintegModelIDJSLowercase(t *testing.T) {
	if !isGemmaModel("GEMMA-3") {
		t.Error("GEMMA-3 is a Gemma model")
	}
	if got := maxThinkingTokensForModel("gem\u0130n\u0130-2.5-PRO"); got != 32768 {
		t.Errorf("maxThinkingTokensForModel = %d, want 32768 (2.5-pro)", got)
	}
	if got := maxThinkingTokensForModel("GEM\u0130N\u0130-3-PRO-\u0130MAGE"); got != 24576 {
		t.Errorf("U+0130 lowercases to i + U+0307 in JS: %d, want 24576", got)
	}
}
