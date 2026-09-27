package fishaudio

import "testing"

// TestProviderSupportsSpeechAndTranscription mirrors the TypeScript SDK's
// "createFishAudio" describe block: speech models resolve for any model ID,
// and the transcription model defaults to "transcribe-1" (fish-audio-provider.test.ts).
func TestProviderSupportsSpeechAndTranscription(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if p.Name() != "fish-audio" {
		t.Fatalf("Name() = %q", p.Name())
	}

	speech, err := p.SpeechModel(ModelS2Pro)
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	if speech.ModelID() != ModelS2Pro || speech.Provider() != "fish-audio.speech" || speech.SpecificationVersion() != "v4" {
		t.Fatalf("speech metadata mismatch: %#v", speech)
	}

	transcription, err := p.TranscriptionModel("")
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	if transcription.ModelID() != ModelTranscribe1 {
		t.Fatalf("default transcription model = %q, want %q", transcription.ModelID(), ModelTranscribe1)
	}
	if transcription.Provider() != "fish-audio.transcription" {
		t.Fatalf("transcription provider = %q", transcription.Provider())
	}
}

// TestProviderFallsBackToEnvAPIKey mirrors the TypeScript SDK's loadApiKey
// fallback to the FISH_AUDIO_API_KEY environment variable when no explicit
// apiKey is configured.
func TestProviderFallsBackToEnvAPIKey(t *testing.T) {
	t.Setenv("FISH_AUDIO_API_KEY", "env-api-key")

	p := New(Config{})
	if p.config.APIKey != "env-api-key" {
		t.Fatalf("config.APIKey = %q, want env-api-key", p.config.APIKey)
	}

	// An explicitly configured key takes precedence over the environment.
	p = New(Config{APIKey: "explicit-key"})
	if p.config.APIKey != "explicit-key" {
		t.Fatalf("config.APIKey = %q, want explicit-key", p.config.APIKey)
	}
}

// TestProviderUnsupportedModelTypes mirrors "should throw for unsupported model types".
func TestProviderUnsupportedModelTypes(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if _, err := p.LanguageModel("s1"); err == nil {
		t.Fatal("expected LanguageModel error")
	}
	if _, err := p.EmbeddingModel("s1"); err == nil {
		t.Fatal("expected EmbeddingModel error")
	}
	if _, err := p.ImageModel("s1"); err == nil {
		t.Fatal("expected ImageModel error")
	}
	if _, err := p.RerankingModel("s1"); err == nil {
		t.Fatal("expected RerankingModel error")
	}
}
