package revai

import "testing"

func TestProviderSupportsTranscription(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})

	if p.Name() != "revai" {
		t.Fatalf("Name() = %q", p.Name())
	}

	transcription, err := p.TranscriptionModel(ModelMachine)
	if err != nil {
		t.Fatalf("TranscriptionModel: %v", err)
	}
	if transcription.ModelID() != ModelMachine || transcription.Provider() != "revai.transcription" || transcription.SpecificationVersion() != "v4" {
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
	if _, err := p.SpeechModel("m"); err == nil {
		t.Fatal("expected SpeechModel error")
	}
	if _, err := p.RerankingModel("m"); err == nil {
		t.Fatal("expected RerankingModel error")
	}
}
