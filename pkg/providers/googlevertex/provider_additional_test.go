package googlevertex

import (
	"testing"
)

func TestVertexProvider_CreateAliasesAndClient(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
	}
	p1, err := CreateGoogleVertex(cfg)
	if err != nil {
		t.Fatalf("CreateGoogleVertex() error = %v", err)
	}
	if p1 == nil || p1.Client() == nil {
		t.Fatal("CreateGoogleVertex returned nil provider or nil client")
	}

	p2, err := CreateVertex(cfg)
	if err != nil {
		t.Fatalf("CreateVertex() error = %v", err)
	}
	if p2 == nil || p2.Client() == nil {
		t.Fatal("CreateVertex returned nil provider or nil client")
	}
}

func TestVertexProvider_ModelFactoriesAndUnsupportedMethods(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	em, err := p.EmbeddingModel("text-embedding-005")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	if em.Provider() != "google-vertex" {
		t.Fatalf("EmbeddingModel provider = %q, want google-vertex", em.Provider())
	}
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("EmbeddingModel(\"\") expected error")
	}

	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
	if _, err := p.VideoModel(""); err == nil {
		t.Fatal("VideoModel(\"\") expected validation error")
	}
	if _, err := p.VideoModel("veo-3.0-generate-preview"); err == nil {
		t.Fatal("VideoModel expected not implemented error")
	}
}
