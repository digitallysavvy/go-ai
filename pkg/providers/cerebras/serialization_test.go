package cerebras

import "testing"

func TestCerebrasSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.cerebras.ai/v1"})
	model, err := p.LanguageModel("llama3.1-8b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := model.(*LanguageModel).Serialize()
	if serialized.Provider != "cerebras" || serialized.ModelID != "llama3.1-8b" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "cerebras" || restored.ModelID() != "llama3.1-8b" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
