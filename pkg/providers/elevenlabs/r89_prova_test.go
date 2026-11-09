package elevenlabs

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR89provaCompositionPlanConflictOrder: with both prompt-only options
// next to a composition plan, TS reports forceInstrumental first (TS
// elevenlabs-speech-model.ts getAudioGenerationArgs loops
// ['forceInstrumental', 'musicLengthMs']).
func TestR89provaCompositionPlanConflictOrder(t *testing.T) {
	p, calls := genServer(t)
	_, err := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "x", ProviderOptions: el(
		"compositionPlan", m("chunks", []interface{}{}), "musicLengthMs", 5000.0, "forceInstrumental", true)})
	if err == nil || !strings.Contains(err.Error(), "ElevenLabs music forceInstrumental cannot be combined with compositionPlan.") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %d", len(*calls))
	}
}

// TestR89provaKeytermLengthUTF16: z.string().max(20) counts UTF-16 code
// units (TS elevenlabs-transcription-model-options.ts streaming.keyterms),
// so 11 emoji (22 units) are too long.
func TestR89provaKeytermLengthUTF16(t *testing.T) {
	long := strings.Repeat("😀", 11)
	err := validateTranscriptionOptions(map[string]interface{}{"elevenlabs": map[string]interface{}{
		"streaming": map[string]interface{}{"keyterms": []interface{}{long}},
	}})
	if err == nil {
		t.Fatal("expected an error for a 22-unit keyterm")
	}
	if err := validateTranscriptionOptions(map[string]interface{}{"elevenlabs": map[string]interface{}{
		"streaming": map[string]interface{}{"keyterms": []interface{}{strings.Repeat("😀", 10)}},
	}}); err != nil {
		t.Fatalf("20 units: %v", err)
	}
}
