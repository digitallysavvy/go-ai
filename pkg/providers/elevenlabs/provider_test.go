package elevenlabs

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestProviderFallsBackToEnvAPIKey mirrors the TypeScript SDK's loadApiKey
// fallback to the ELEVENLABS_API_KEY environment variable when no explicit
// apiKey is configured.
func TestProviderFallsBackToEnvAPIKey(t *testing.T) {
	t.Setenv("ELEVENLABS_API_KEY", "env-api-key")

	p := New(Config{})
	if p.config.APIKey != "env-api-key" {
		t.Fatalf("config.APIKey = %q, want env-api-key", p.config.APIKey)
	}

	p = New(Config{APIKey: "explicit-key"})
	if p.config.APIKey != "explicit-key" {
		t.Fatalf("config.APIKey = %q, want explicit-key", p.config.APIKey)
	}
}

func TestProviderFactoriesAndUnsupported(t *testing.T) {
	p := New(Config{APIKey: "key"})
	if p.Name() != "elevenlabs" {
		t.Fatalf("Name = %q", p.Name())
	}

	if lm, err := p.LanguageModel("x"); lm != nil || err == nil {
		t.Fatalf("LanguageModel expected unsupported error, got model=%v err=%v", lm, err)
	}
	if em, err := p.EmbeddingModel("x"); em != nil || err == nil {
		t.Fatalf("EmbeddingModel expected unsupported error, got model=%v err=%v", em, err)
	}
	if im, err := p.ImageModel("x"); im != nil || err == nil {
		t.Fatalf("ImageModel expected unsupported error, got model=%v err=%v", im, err)
	}
	tm, err := p.TranscriptionModel("scribe_v1")
	if err != nil || tm == nil {
		t.Fatalf("TranscriptionModel: model=%v err=%v", tm, err)
	}
	if tm.ModelID() != "scribe_v1" {
		t.Fatalf("TranscriptionModel ModelID = %q", tm.ModelID())
	}
	if rm, err := p.RerankingModel("x"); rm != nil || err == nil {
		t.Fatalf("RerankingModel expected unsupported error, got model=%v err=%v", rm, err)
	}

	sm, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	if sm.ModelID() != "" {
		t.Fatalf("model ID = %q", sm.ModelID())
	}
}

func TestSpeechModelBuildRequestBody(t *testing.T) {
	p := New(Config{APIKey: "key"})
	m := NewSpeechModel(p, "eleven_flash_v2")
	speed := 0.77

	body := m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text:  "hello",
		Speed: &speed,
	})
	if body["text"] != "hello" {
		t.Fatalf("text = %#v", body["text"])
	}
	if body["model_id"] != "eleven_flash_v2" {
		t.Fatalf("model_id = %#v", body["model_id"])
	}
	vs, ok := body["voice_settings"].(map[string]interface{})
	if !ok {
		t.Fatalf("voice_settings missing: %#v", body["voice_settings"])
	}
	if vs["speed"] != speed {
		t.Fatalf("speed = %#v, want %v", vs["speed"], speed)
	}
}
