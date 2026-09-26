package baseten

import "testing"

func TestNew_DefaultsAndName(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p == nil || p.Provider == nil {
		t.Fatal("provider should be initialized")
	}
	if p.Name() != "baseten" {
		t.Fatalf("Name = %q, want baseten", p.Name())
	}
}

func TestLanguageModelFactory(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m, err := p.LanguageModel("gpt-4o")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if m.Provider() != "baseten.chat" {
		t.Fatalf("Provider = %q, want baseten.chat", m.Provider())
	}
	if m.ModelID() != "gpt-4o" {
		t.Fatalf("ModelID = %q, want gpt-4o", m.ModelID())
	}
}

func TestNew_DefaultBaseURL(t *testing.T) {
	// c8cc95f: the default Model APIs base URL is inference.baseten.co, not
	// the old bridge.baseten.co.
	if DefaultBaseURL != "https://inference.baseten.co/v1" {
		t.Fatalf("DefaultBaseURL = %q", DefaultBaseURL)
	}
}

func TestEmbeddingModel_RequiresModelURL(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("expected an error when no ModelURL is configured")
	}
}

func TestEmbeddingModel_RejectsNonSyncModelURL(t *testing.T) {
	p := New(Config{APIKey: "k", ModelURL: "https://model-abc.api.baseten.co/predict"})
	if _, err := p.EmbeddingModel(""); err == nil {
		t.Fatal("expected an error for a non-/sync ModelURL")
	}
}

func TestEmbeddingModel_AcceptsSyncModelURL(t *testing.T) {
	p := New(Config{APIKey: "k", ModelURL: "https://model-abc.api.baseten.co/sync"})
	m, err := p.EmbeddingModel("")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	if m.MaxEmbeddingsPerCall() != 128 {
		t.Fatalf("MaxEmbeddingsPerCall() = %d, want 128", m.MaxEmbeddingsPerCall())
	}
}

func TestLanguageModel_RejectsPredictModelURL(t *testing.T) {
	p := New(Config{APIKey: "k", ModelURL: "https://model-abc.api.baseten.co/predict"})
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected an error for a /predict ModelURL on chat models")
	}
}

func TestLanguageModel_AcceptsSyncV1ModelURL(t *testing.T) {
	p := New(Config{APIKey: "k", ModelURL: "https://model-abc.api.baseten.co/sync/v1"})
	m, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if m.Provider() != "baseten.chat" {
		t.Fatalf("Provider() = %q", m.Provider())
	}
}
