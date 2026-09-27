package huggingface

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from TS HuggingFaceResponsesLanguageModel's static
// [WORKFLOW_SERIALIZE]/[WORKFLOW_DESERIALIZE] (exercised indirectly by the
// TS workflow serialization tests): serializes to {modelId, config} and
// reconstructs `new HuggingFaceResponsesLanguageModel(modelId, config)`.
func TestHuggingFaceSerializeAndDeserializeModel(t *testing.T) {
	p := New(Config{APIKey: "secret", BaseURL: "https://example.com/v1", Headers: map[string]string{"X-Test": "1"}})

	model, err := p.LanguageModel("deepseek-ai/DeepSeek-V3-0324")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	serializable, ok := model.(provider.SerializableModel)
	if !ok {
		t.Fatalf("model does not implement provider.SerializableModel")
	}

	serialized := serializable.Serialize()
	if serialized.Provider != "huggingface.responses" || serialized.ModelID != "deepseek-ai/DeepSeek-V3-0324" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	if serialized.Config["BaseURL"] != "https://example.com/v1" {
		t.Fatalf("BaseURL should be serialized: %#v", serialized.Config)
	}
	headers, ok := serialized.Config["Headers"].(map[string]interface{})
	if !ok || headers["X-Test"] != "1" {
		t.Fatalf("Headers should be serialized: %#v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel() error: %v", err)
	}
	if restored.Provider() != "huggingface.responses" || restored.ModelID() != "deepseek-ai/DeepSeek-V3-0324" {
		t.Fatalf("unexpected restored model provider=%q model=%q", restored.Provider(), restored.ModelID())
	}

	asMap, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("provider.SerializeModel() error: %v", err)
	}
	if asMap.Provider != "huggingface.responses" || asMap.ModelID != "deepseek-ai/DeepSeek-V3-0324" {
		t.Fatalf("unexpected provider.SerializeModel result: %#v", asMap)
	}
}
