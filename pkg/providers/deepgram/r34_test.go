package deepgram

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS: `Deepgram speech model "${modelId}" requires a \`voice\` to be set (e.g. voice: 'thalia').`
func TestR34_BareFamilyWithoutVoiceErrorTextMatchesTS(t *testing.T) {
	m := NewSpeechModel(New(Config{APIKey: "k"}), "aura-2")
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	want := "Deepgram speech model \"aura-2\" requires a `voice` to be set (e.g. voice: 'thalia')."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
