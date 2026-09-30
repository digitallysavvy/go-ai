package voyage

import "testing"

func TestVoyageSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.EmbeddingModel("voyage-3")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := model.(*EmbeddingModel).Serialize()
	if serialized.Provider != "voyage.embedding" || serialized.ModelID != "voyage-3" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "voyage.embedding" || restored.ModelID() != "voyage-3" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
