package cohere

import "testing"

func TestCohereSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.cohere.ai/v1"})
	modelAny, err := p.LanguageModel("command-r-plus")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "cohere" || serialized.ModelID != "command-r-plus" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "cohere" || restored.ModelID() != "command-r-plus" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
