package fishaudio

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS 203a0e09eb catalog refactor (ruling 13): normalizeLoudness is rejected
// only for s1 (TS definition.normalizeLoudness === false), so an unknown ID
// forwards it; ignoreTimestamps defaults to false for every ID.
func TestPortProvcNormalizeLoudnessOnUnknownIDs(t *testing.T) {
	on := true
	for id, wantSent := range map[string]bool{"s1": false, "s2-pro": true, "s2.1-pro-free": true, "future-model": true, "": true} {
		m := &SpeechModel{provider: New(Config{APIKey: "k"}), modelID: id}
		body, warnings := m.buildRequestBody(&provider.SpeechGenerateOptions{
			Text:            "x",
			ProviderOptions: map[string]interface{}{"fishAudio": SpeechModelOptions{NormalizeLoudness: &on}},
		})
		prosody, _ := body["prosody"].(map[string]interface{})
		_, sent := prosody["normalize_loudness"]
		warned := false
		for _, w := range warnings {
			warned = warned || w.Feature == "providerOptions.fishAudio.normalizeLoudness"
		}
		if sent != wantSent || warned == wantSent {
			t.Errorf("model %q: sent = %v warned = %v, want sent = %v", id, sent, warned, wantSent)
		}
	}
}

func TestPortProvcIgnoreTimestampsDefaultOnUnknownIDs(t *testing.T) {
	for _, id := range []string{"asr-1", "future-model", ""} {
		m := &TranscriptionModel{provider: New(Config{APIKey: "k"}), modelID: id}
		buf, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
		if err != nil {
			t.Fatal(err)
		}
		if body := buf.String(); !strings.Contains(body, "name=\"ignore_timestamps\"\r\n\r\nfalse\r\n") {
			t.Errorf("model %q: ignore_timestamps is not false by default:\n%s", id, body)
		}
	}
}
