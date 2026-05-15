package perplexity

import "testing"

func TestPerplexitySerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.perplexity.ai"})
	modelAny, err := p.LanguageModel("llama-3.1-sonar-small-128k-online")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "perplexity" || serialized.ModelID != "llama-3.1-sonar-small-128k-online" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "perplexity" || restored.ModelID() != "llama-3.1-sonar-small-128k-online" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
