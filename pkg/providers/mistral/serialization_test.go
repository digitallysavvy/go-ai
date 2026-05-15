package mistral

import "testing"

func TestMistralSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.mistral.ai"})
	modelAny, err := p.LanguageModel("mistral-small-latest")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "mistral" || serialized.ModelID != "mistral-small-latest" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "mistral" || restored.ModelID() != "mistral-small-latest" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
