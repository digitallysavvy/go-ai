package deepinfra

import "testing"

func TestDeepInfraSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.LanguageModel("meta-llama/Llama-3-8b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := model.(*LanguageModel).Serialize()
	if serialized.Provider != "deepinfra" || serialized.ModelID != "meta-llama/Llama-3-8b" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeLanguageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeLanguageModel error = %v", err)
	}
	if restored.Provider() != "deepinfra" || restored.ModelID() != "meta-llama/Llama-3-8b" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestDeepInfraSerializeAndDeserializeImageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.ImageModel("stabilityai/sd3.5")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := model.(*ImageModel).Serialize()
	if serialized.Provider != "deepinfra" || serialized.ModelID != "stabilityai/sd3.5" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "deepinfra" || restored.ModelID() != "stabilityai/sd3.5" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
