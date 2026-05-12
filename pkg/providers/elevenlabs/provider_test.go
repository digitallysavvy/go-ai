package elevenlabs

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

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
	if tm, err := p.TranscriptionModel("x"); tm != nil || err == nil {
		t.Fatalf("TranscriptionModel expected unsupported error, got model=%v err=%v", tm, err)
	}
	if rm, err := p.RerankingModel("x"); rm != nil || err == nil {
		t.Fatalf("RerankingModel expected unsupported error, got model=%v err=%v", rm, err)
	}

	sm, err := p.SpeechModel("")
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	if sm.ModelID() != "eleven_multilingual_v2" {
		t.Fatalf("default model ID = %q", sm.ModelID())
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
	if vs["stability"] != speed {
		t.Fatalf("stability = %#v, want %v", vs["stability"], speed)
	}
}
