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
