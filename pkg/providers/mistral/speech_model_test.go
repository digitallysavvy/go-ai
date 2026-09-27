package mistral

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestMistralSpeechModelDoGenerate guards ba433f7: non-streaming Voxtral TTS
// with a saved voice ID.
func TestMistralSpeechModelDoGenerate(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	audioBytes := []byte("fake-audio-bytes")
	audioB64 := base64.StdEncoding.EncodeToString(audioBytes)

	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return mistralJSONResponse(`{"audio_data":"` + audioB64 + `"}`), nil
	})

	m := NewSpeechModel(p, "voxtral-mini-tts-latest")
	if m.SpecificationVersion() != "v4" || m.Provider() != "mistral" || m.ModelID() != "voxtral-mini-tts-latest" {
		t.Fatalf("metadata mismatch: %s %s %s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}

	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hello world",
		Voice: "friendly-voice",
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v1/audio/speech" {
		t.Fatalf("path = %q, want /v1/audio/speech", seenPath)
	}
	if seenBody["model"] != "voxtral-mini-tts-latest" || seenBody["input"] != "hello world" || seenBody["voice_id"] != "friendly-voice" {
		t.Fatalf("request body mismatch: %#v", seenBody)
	}
	if _, ok := seenBody["ref_audio"]; ok {
		t.Fatalf("ref_audio should be omitted when unset, got %v", seenBody["ref_audio"])
	}
	if seenBody["response_format"] != "mp3" || seenBody["stream"] != false {
		t.Fatalf("request body mismatch: %#v", seenBody)
	}
	if string(result.Audio) != string(audioBytes) {
		t.Fatalf("audio = %q, want %q", result.Audio, audioBytes)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", result.Warnings)
	}
}

// TestMistralSpeechModelRefAudioTakesPrecedence guards the one-off reference
// audio path: ref_audio must be forwarded and take precedence over voice_id.
func TestMistralSpeechModelRefAudioTakesPrecedence(t *testing.T) {
	var seenBody map[string]interface{}
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return mistralJSONResponse(`{"audio_data":"` + base64.StdEncoding.EncodeToString([]byte("x")) + `"}`), nil
	})
	m := NewSpeechModel(p, "voxtral-mini-tts-latest")

	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "hi",
		Voice: "some-voice",
		ProviderOptions: map[string]interface{}{
			"mistral": map[string]interface{}{"refAudio": "base64-ref-audio"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenBody["ref_audio"] != "base64-ref-audio" {
		t.Fatalf("ref_audio = %v, want base64-ref-audio", seenBody["ref_audio"])
	}
	if _, ok := seenBody["voice_id"]; ok {
		t.Fatalf("voice_id should be omitted when ref_audio is set, got %v", seenBody["voice_id"])
	}
}

// TestMistralSpeechModelUnsupportedOptionsWarn guards the instructions/speed/
// language warnings mirrored from TS mistral-speech-model.ts.
func TestMistralSpeechModelUnsupportedOptionsWarn(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return mistralJSONResponse(`{"audio_data":"` + base64.StdEncoding.EncodeToString([]byte("x")) + `"}`), nil
	})
	m := NewSpeechModel(p, "voxtral-mini-tts-latest")

	speed := 1.5
	result, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "hi",
		Instructions: "speak fast",
		Speed:        &speed,
		Language:     "fr",
		OutputFormat: "bogus",
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	features := map[string]bool{}
	for _, w := range result.Warnings {
		features[w.Feature] = true
	}
	for _, want := range []string{"instructions", "speed", "language", "outputFormat"} {
		if !features[want] {
			t.Errorf("expected warning for %s, got warnings: %+v", want, result.Warnings)
		}
	}
}
