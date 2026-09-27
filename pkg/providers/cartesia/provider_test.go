package cartesia

import "testing"

func TestProviderSupportsSpeechAndTranscription(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if p.Name() != "cartesia" {
		t.Fatalf("Name() = %q", p.Name())
	}

	speech, err := p.SpeechModel(ModelSonic35)
	if err != nil {
		t.Fatalf("SpeechModel: %v", err)
	}
	if speech.ModelID() != ModelSonic35 || speech.Provider() != "cartesia.speech" || speech.SpecificationVersion() != "v4" {
		t.Fatalf("speech metadata mismatch: %#v", speech)
	}

	transcription, err := p.TranscriptionModel(ModelInkWhisper)
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	if transcription.ModelID() != ModelInkWhisper || transcription.Provider() != "cartesia.transcription" {
		t.Fatalf("transcription metadata mismatch: %#v", transcription)
	}
}

func TestProviderUnsupportedModelTypes(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if _, err := p.LanguageModel("m"); err == nil {
		t.Fatal("expected LanguageModel error")
	}
	if _, err := p.EmbeddingModel("m"); err == nil {
		t.Fatal("expected EmbeddingModel error")
	}
	if _, err := p.ImageModel("m"); err == nil {
		t.Fatal("expected ImageModel error")
	}
	if _, err := p.RerankingModel("m"); err == nil {
		t.Fatal("expected RerankingModel error")
	}
}
