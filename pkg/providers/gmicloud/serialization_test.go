package gmicloud

import "testing"

func TestGMICloudSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.LanguageModel("deepseek-ai/DeepSeek-R1")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := model.(*LanguageModel).Serialize()
	if serialized.Provider != "gmicloud" || serialized.ModelID != "deepseek-ai/DeepSeek-R1" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "gmicloud" || restored.ModelID() != "deepseek-ai/DeepSeek-R1" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
