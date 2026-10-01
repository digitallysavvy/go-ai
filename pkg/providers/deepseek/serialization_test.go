package deepseek

import "testing"

func TestDeepseekSerializeAndDeserializeLanguageModel(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://api.deepseek.com"})
	modelAny, err := p.LanguageModel("deepseek-chat")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "deepseek" || serialized.ModelID != "deepseek-chat" || serialized.Config == nil {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "deepseek" || restored.ModelID() != "deepseek-chat" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
