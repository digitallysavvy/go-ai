package azure

import "testing"

// toLowerCase class sweep (round 86b): TS isMAITranscribeStreaming is
// modelId.toLowerCase() === 'mai-transcribe-2-streaming'; JS does not fold
// U+017F to "s" (Go strings.EqualFold did), and lowercases U+0130 to
// "i\u0307".
func TestR86bintegMAIModelIDsJSLowercase(t *testing.T) {
	if !isMAITranscribeStreaming("MAI-Transcribe-2-Streaming") {
		t.Error("mixed case is the streaming model")
	}
	if isMAITranscribeStreaming("mai-tran\u017fcribe-2-\u017ftreaming") {
		t.Error("U+017F is not s in JS toLowerCase")
	}
}
