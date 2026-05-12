package vercel

import "testing"

func TestNew_DefaultsAndName(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p == nil || p.Provider == nil {
		t.Fatal("provider should be initialized")
	}
	if p.Name() != "vercel" {
		t.Fatalf("Name = %q, want vercel", p.Name())
	}
}

func TestEmbeddingModelFactory(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m, err := p.EmbeddingModel("text-embedding-3-small")
	if err != nil {
		t.Fatalf("EmbeddingModel: %v", err)
	}
	if m.Provider() != "openai" {
		t.Fatalf("Provider = %q, want openai", m.Provider())
	}
	if m.ModelID() != "text-embedding-3-small" {
		t.Fatalf("ModelID = %q", m.ModelID())
	}
}
