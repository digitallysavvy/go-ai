package groq

import "testing"

func TestGroqSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.groq.com/openai"})
	modelAny, err := p.LanguageModel("llama-3.3-70b-versatile")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "groq" || serialized.ModelID != "llama-3.3-70b-versatile" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "groq" || restored.ModelID() != "llama-3.3-70b-versatile" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
