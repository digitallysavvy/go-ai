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
	if m.Provider() != "openai" {
		t.Fatalf("Provider = %q, want openai", m.Provider())
	}
	if m.ModelID() != "gpt-4o" {
		t.Fatalf("ModelID = %q, want gpt-4o", m.ModelID())
	}
}
