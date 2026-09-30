package alibaba

import "testing"

func TestAlibabaSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://example.com/v1"})
	model, err := p.LanguageModel("qwen-plus")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := model.(*LanguageModel).Serialize()
	if serialized.Provider != "alibaba" || serialized.ModelID != "qwen-plus" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeLanguageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeLanguageModel error = %v", err)
	}
	if restored.Provider() != "alibaba" || restored.ModelID() != "qwen-plus" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestAlibabaSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.EmbeddingModel("text-embedding-v3")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := model.(*EmbeddingModel).Serialize()
	if serialized.Provider != "alibaba.embedding" || serialized.ModelID != "text-embedding-v3" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "alibaba.embedding" || restored.ModelID() != "text-embedding-v3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
