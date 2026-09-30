package anthropicaws

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestAnthropicAWSSerializeAndDeserializeLanguageModel(t *testing.T) {
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "ws-1", APIKey: "k"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.LanguageModel("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel error = %v", err)
	}
	if serialized.Provider != "anthropic-aws.messages" || serialized.ModelID != "claude-sonnet-4-20250514" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel error = %v", err)
	}
	if restored.Provider() != "anthropic-aws.messages" || restored.ModelID() != "claude-sonnet-4-20250514" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
