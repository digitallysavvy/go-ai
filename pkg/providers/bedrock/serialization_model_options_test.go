package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestBedrockSerializeDeserializeWithModelOptions(t *testing.T) {
	budget := 2048
	p := New(Config{
		AWSAccessKeyID:     "k",
		AWSSecretAccessKey: "s",
		Region:             "us-east-1",
	})
	modelAny, err := p.LanguageModelWithOptions("anthropic.claude-3-haiku-20240307-v1:0", &ModelOptions{
		Thinking: &ThinkingConfig{
			Type:         ThinkingTypeEnabled,
			BudgetTokens: &budget,
		},
		ReasoningConfig: &ReasoningConfig{
			Type:               "enabled",
			MaxReasoningEffort: "medium",
		},
		ServiceTier: "default",
	})
	if err != nil {
		t.Fatalf("LanguageModelWithOptions error = %v", err)
	}
	model := modelAny.(*LanguageModel)

	serialized := model.Serialize()
	if serialized.Provider != "amazon-bedrock" || serialized.ModelID == "" {
		t.Fatalf("serialized mismatch: %#v", serialized)
	}
	rawOpts, ok := serialized.Config["modelOptions"].(map[string]interface{})
	if !ok || rawOpts["serviceTier"] != "default" {
		t.Fatalf("serialized model options missing: %#v", serialized.Config)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "amazon-bedrock" || restored.ModelID() != "anthropic.claude-3-haiku-20240307-v1:0" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func TestBedrockThinkingAndReasoningConfigShapes(t *testing.T) {
	if ThinkingTypeAdaptive == "" || ThinkingTypeEnabled == "" || ThinkingTypeDisabled == "" {
		t.Fatal("thinking type constants should be non-empty")
	}

	budget := 4096
	cfg := ModelOptions{
		Thinking: &ThinkingConfig{
			Type:         ThinkingTypeEnabled,
			BudgetTokens: &budget,
		},
		ReasoningConfig: &ReasoningConfig{
			Type:               "enabled",
			BudgetTokens:       &budget,
			MaxReasoningEffort: "high",
			Display:            "full",
		},
		AdditionalModelRequestFields: map[string]interface{}{"x": 1},
		ServiceTier:                  "priority",
	}
	if cfg.Thinking == nil || cfg.Thinking.Type != ThinkingTypeEnabled || cfg.Thinking.BudgetTokens == nil || *cfg.Thinking.BudgetTokens != 4096 {
		t.Fatalf("thinking config mismatch: %#v", cfg.Thinking)
	}
	if cfg.ReasoningConfig == nil || cfg.ReasoningConfig.MaxReasoningEffort != "high" || cfg.ServiceTier != "priority" {
		t.Fatalf("reasoning config mismatch: %#v", cfg)
	}
}

func TestBedrockEmbeddingModelSerializeRoundTrip(t *testing.T) {
	p := New(Config{
		AWSAccessKeyID:     "k",
		AWSSecretAccessKey: "s",
		Region:             "us-east-1",
	})
	modelAny, err := p.EmbeddingModelWithOptions("amazon.titan-embed-text-v2:0", &EmbeddingOptions{
		ModelFamily: "titan",
	})
	if err != nil {
		t.Fatalf("EmbeddingModelWithOptions error = %v", err)
	}
	model := modelAny.(*EmbeddingModel)

	serialized := model.Serialize()
	if serialized.Provider != "amazon-bedrock" || serialized.ModelID != "amazon.titan-embed-text-v2:0" {
		t.Fatalf("serialized mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["AWSAccessKeyID"]; ok {
		t.Fatalf("serialized config should not include credentials: %#v", serialized.Config)
	}
	rawOpts, ok := serialized.Config["modelOptions"].(map[string]interface{})
	if !ok || rawOpts["modelFamily"] != "titan" {
		t.Fatalf("serialized model options missing: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "amazon-bedrock" || restored.ModelID() != "amazon.titan-embed-text-v2:0" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{
		Provider: serialized.Provider,
		ModelID:  serialized.ModelID,
		Config:   serialized.Config,
	})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "amazon.titan-embed-text-v2:0" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestBedrockImageModelSerializeRoundTrip(t *testing.T) {
	p := New(Config{
		AWSAccessKeyID:     "k",
		AWSSecretAccessKey: "s",
		Region:             "us-east-1",
	})
	modelAny, err := p.ImageModel("amazon.titan-image-generator-v2:0")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	model := modelAny.(*ImageModel)

	serialized := model.Serialize()
	if serialized.Provider != "amazon-bedrock" || serialized.ModelID != "amazon.titan-image-generator-v2:0" {
		t.Fatalf("serialized mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["AWSSecretAccessKey"]; ok {
		t.Fatalf("serialized config should not include credentials: %#v", serialized.Config)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "amazon-bedrock" || restored.ModelID() != "amazon.titan-image-generator-v2:0" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{
		Provider: serialized.Provider,
		ModelID:  serialized.ModelID,
		Config:   serialized.Config,
	})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "amazon.titan-image-generator-v2:0" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}
