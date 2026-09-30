package baseten

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestBasetenSerializeAndDeserializeChatModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel error = %v", err)
	}
	if serialized.Provider != "baseten.chat" || serialized.ModelID != "chat" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel error = %v", err)
	}
	if restored.Provider() != "baseten.chat" || restored.ModelID() != "chat" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
