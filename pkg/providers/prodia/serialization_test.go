package prodia

import "testing"

func TestProdiaSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.LanguageModel(LanguageModelNanoBananaImgToImgV2)
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := model.(*ProdiaLanguageModel).Serialize()
	if serialized.Provider != "prodia.language" || serialized.ModelID != LanguageModelNanoBananaImgToImgV2 {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeLanguageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeLanguageModel error = %v", err)
	}
	if restored.Provider() != "prodia.language" || restored.ModelID() != LanguageModelNanoBananaImgToImgV2 {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestProdiaSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.ImageModel("sd3-medium")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := model.(*ImageModel).Serialize()
	if serialized.Provider != "prodia.image" || serialized.ModelID != "sd3-medium" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "prodia.image" || restored.ModelID() != "sd3-medium" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
