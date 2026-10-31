package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestProviderIDsFollowTSConfigProvider pins Provider() to the TS
// config.provider string for each model kind, and checks that payloads
// serialized before the change (bare provider names) still restore.
func TestProviderIDsFollowTSConfigProvider(t *testing.T) {
	type tc struct{ oldKey, newKey, want string }
	lang := []tc{
		{"groq", "groq.chat", "groq.chat"},
		{"cerebras", "cerebras.chat", "cerebras.chat"},
		{"fireworks", "fireworks.chat", "fireworks.chat"},
		{"together", "togetherai.chat", "togetherai.chat"},
		{"alibaba", "alibaba.chat", "alibaba.chat"},
		{"deepinfra", "deepinfra.chat", "deepinfra.chat"},
		{"moonshot", "moonshotai.chat", "moonshotai.chat"},
		{"deepseek", "deepseek.chat", "deepseek.chat"},
		{"gmicloud", "gmicloud.chat", "gmicloud.chat"},
		{"zai", "zai.chat", "zai.chat"},
		{"cohere", "cohere.chat", "cohere.chat"},
		{"mistral", "mistral.chat", "mistral.chat"},
	}
	for _, c := range lang {
		for _, key := range []string{c.oldKey, c.newKey} {
			m, err := provider.DeserializeModel(provider.SerializedModel{Provider: key, ModelID: modelIDFor(key), Config: configFor(key)})
			if err != nil {
				t.Fatalf("DeserializeModel(%q): %v", key, err)
			}
			if m.Provider() != c.want {
				t.Errorf("language %q restored Provider() = %q, want %q", key, m.Provider(), c.want)
			}
		}
	}
	tr := []tc{
		{"groq", "groq.transcription", "groq.transcription"},
		{"deepgram", "deepgram.transcription", "deepgram.transcription"},
		{"assemblyai", "assemblyai.transcription", "assemblyai.transcription"},
		{"gladia", "gladia.transcription", "gladia.transcription"},
		{"elevenlabs", "elevenlabs.transcription", "elevenlabs.transcription"},
		{"mistral", "mistral.transcription", "mistral.transcription"},
		{"openai", "openai.transcription", "openai.transcription"},
	}
	for _, c := range tr {
		for _, key := range []string{c.oldKey, c.newKey} {
			m, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: key, ModelID: modelIDFor(key), Config: configFor(key)})
			if err != nil {
				t.Fatalf("DeserializeTranscriptionModel(%q): %v", key, err)
			}
			if m.Provider() != c.want {
				t.Errorf("transcription %q restored Provider() = %q, want %q", key, m.Provider(), c.want)
			}
		}
	}
}
