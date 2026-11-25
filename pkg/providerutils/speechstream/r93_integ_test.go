package speechstream

import (
	"strings"
	"testing"
)

// TestR93integFinishUsageCountsAreJSNumbers: TS
// Experimental_SpeechModelV4StreamUsage counts are JS numbers, so a
// fractional gateway count is kept as sent, not truncated (main ruling,
// round 93; SpeechStreamUsage is new in v0.6).
func TestR93integFinishUsageCountsAreJSNumbers(t *testing.T) {
	got, ok := ParsePart(`{"type":"finish","usage":{"inputCharacters":12.5,"inputTokens":3.25,"outputTokens":1e21}}`)
	if !ok || got.Usage == nil {
		t.Fatalf("part = %+v, ok = %v", got, ok)
	}
	u := got.Usage
	if u.InputCharacters == nil || *u.InputCharacters != 12.5 || u.InputTokens == nil || *u.InputTokens != 3.25 ||
		u.OutputTokens == nil || *u.OutputTokens != 1e21 {
		t.Fatalf("usage = %+v", u)
	}
	text, ok := SerializePart(got)
	if !ok || !strings.Contains(text, `"inputCharacters":12.5`) || !strings.Contains(text, `"inputTokens":3.25`) {
		t.Fatalf("serialized = %s", text)
	}
}
