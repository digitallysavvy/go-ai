package ai

import "testing"

// TS getAudioMediaType lower-cases with String.prototype.toLowerCase, which
// maps U+0130 to "i" plus U+0307 (node: "audi̇o/mpeg"), so the type is not
// audio and the bytes are sniffed; strings.ToLower gave "audio/mpeg".
func TestR94provcAudioMediaTypeJSLowerCase(t *testing.T) {
	if got := getAudioMediaType("AUDİO/MPEG"); got != "" {
		t.Fatalf("getAudioMediaType = %q, want \"\" (not an audio type in TS)", got)
	}
	if got := getAudioMediaType(" Audio/MPEG ; x=1"); got != "audio/mpeg" {
		t.Fatalf("getAudioMediaType = %q, want audio/mpeg", got)
	}
}
