package bedrock

import "testing"

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
	if serialized.Provider != "aws-bedrock" || serialized.ModelID == "" {
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
	if restored.Provider() != "aws-bedrock" || restored.ModelID() != "anthropic.claude-3-haiku-20240307-v1:0" {
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
