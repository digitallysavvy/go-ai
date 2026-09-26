package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// newBedrockModelWithID mirrors newTestBedrockModelWithID for reasoning tests
// that predate that helper's introduction.
func newBedrockModelWithID(modelID string) *LanguageModel {
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})
	return NewLanguageModel(p, modelID)
}

// TestBedrockReasoningBudgetPercentages verifies the percentage-of-maxOutputTokens
// budget mapping (TS provider-utils mapReasoningToProviderBudget) for a
// non-adaptive-thinking Claude model (Sonnet 4.5: maxOutputTokens=64000,
// supportsAdaptiveThinking=false), ported via the additionalModelRequestFields
// thinking.budget_tokens the Converse request carries.
func TestBedrockReasoningBudgetPercentages(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-sonnet-4-5-20250929-v1:0")

	tests := []struct {
		level      types.ReasoningLevel
		wantType   string
		wantBudget int
	}{
		{types.ReasoningMinimal, "enabled", 1280},
		{types.ReasoningLow, "enabled", 6400},
		{types.ReasoningMedium, "enabled", 19200},
		{types.ReasoningHigh, "enabled", 38400},
		{types.ReasoningXHigh, "enabled", 57600},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			args, err := model.getArgs(&provider.GenerateOptions{
				Prompt:    types.Prompt{Text: "hi"},
				Reasoning: &level,
			})
			if err != nil {
				t.Fatalf("getArgs error: %v", err)
			}
			fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
			thinking, _ := fields["thinking"].(map[string]interface{})
			if thinking["type"] != tt.wantType {
				t.Fatalf("thinking.type = %v, want %v", thinking["type"], tt.wantType)
			}
			if thinking["budget_tokens"] != tt.wantBudget {
				t.Fatalf("thinking.budget_tokens = %v, want %v", thinking["budget_tokens"], tt.wantBudget)
			}
		})
	}
}

// TestBedrockReasoningNoneMapsToDisabled verifies ReasoningNone maps to
// thinking:{type:"disabled"} for Anthropic models.
func TestBedrockReasoningNoneMapsToDisabled(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-sonnet-4-5-20250929-v1:0")
	level := types.ReasoningNone
	args, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	// "disabled" thinking is not an enabled/adaptive type, so getArgs never
	// populates additionalModelRequestFields.thinking for it (mirrors TS:
	// only isAnthropicThinkingEnabled branches write `thinking`).
	if _, ok := fields["thinking"]; ok {
		t.Fatalf("expected no thinking field for disabled reasoning, got %v", fields["thinking"])
	}
}

// TestBedrockReasoningDefaultOmitsReasoningConfig verifies ReasoningDefault
// (provider-default) leaves additionalModelRequestFields untouched.
func TestBedrockReasoningDefaultOmitsReasoningConfig(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-sonnet-4-5-20250929-v1:0")
	level := types.ReasoningDefault
	args, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	if _, ok := fields["thinking"]; ok {
		t.Fatalf("expected no thinking field for provider-default reasoning")
	}
}

// TestBedrockAdaptiveThinkingUsesEffort verifies that models with
// supportsAdaptiveThinking (e.g. Opus 5) map reasoning levels to
// thinking:{type:"adaptive"} plus output_config.effort, per TS
// resolveAmazonBedrockReasoningConfig + getArgs.
func TestBedrockAdaptiveThinkingUsesEffort(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-opus-5")

	tests := []struct {
		level      types.ReasoningLevel
		wantEffort string
	}{
		{types.ReasoningMinimal, "low"},
		{types.ReasoningLow, "low"},
		{types.ReasoningMedium, "medium"},
		{types.ReasoningHigh, "high"},
		{types.ReasoningXHigh, "max"},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			args, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level})
			if err != nil {
				t.Fatalf("getArgs error: %v", err)
			}
			fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
			thinking, _ := fields["thinking"].(map[string]interface{})
			if thinking["type"] != "adaptive" {
				t.Fatalf("thinking.type = %v, want adaptive", thinking["type"])
			}
			outputConfig, _ := fields["output_config"].(map[string]interface{})
			if outputConfig["effort"] != tt.wantEffort {
				t.Fatalf("output_config.effort = %v, want %v", outputConfig["effort"], tt.wantEffort)
			}
		})
	}
}

// TestBedrockNonAnthropicReasoningEffort verifies that non-Anthropic models
// (e.g. Nova) map reasoning to additionalModelRequestFields.reasoningConfig.maxReasoningEffort,
// and that ReasoningNone adds no reasoning fields at all (mirrors TS: the
// non-Anthropic branch of resolveAmazonBedrockReasoningConfig only runs when
// reasoning !== 'none').
func TestBedrockNonAnthropicReasoningEffort(t *testing.T) {
	model := newBedrockModelWithID("us.amazon.nova-pro-v1:0")

	tests := []struct {
		level  types.ReasoningLevel
		want   string
		hasKey bool
	}{
		{types.ReasoningMinimal, "low", true},
		{types.ReasoningLow, "low", true},
		{types.ReasoningMedium, "medium", true},
		{types.ReasoningHigh, "high", true},
		{types.ReasoningXHigh, "max", true},
		{types.ReasoningNone, "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			args, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level})
			if err != nil {
				t.Fatalf("getArgs error: %v", err)
			}
			fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
			rc, hasRC := fields["reasoningConfig"].(map[string]interface{})
			if !tt.hasKey {
				if hasRC {
					t.Fatalf("expected no reasoningConfig for 'none', got %v", rc)
				}
				return
			}
			if !hasRC {
				t.Fatalf("expected reasoningConfig, got none")
			}
			if rc["maxReasoningEffort"] != tt.want {
				t.Fatalf("maxReasoningEffort = %v, want %v", rc["maxReasoningEffort"], tt.want)
			}
		})
	}
}

// TestBedrockModelCapabilitiesTable spot-checks bedrockAnthropicModelCapabilities
// against the TS anthropic-language-model.ts#getModelCapabilities table for a
// representative sample of model IDs.
func TestBedrockModelCapabilitiesTable(t *testing.T) {
	tests := []struct {
		modelID                  string
		maxOutputTokens          int
		supportsStructuredOutput bool
		supportsAdaptiveThinking bool
		rejectsSamplingParams    bool
		rejectsForcedToolUse     bool
	}{
		{"anthropic.claude-opus-5-5", 128000, true, true, true, true},
		{"anthropic.claude-opus-5", 128000, true, true, true, false},
		{"anthropic.claude-fable-5-1", 128000, true, true, true, true},
		{"anthropic.claude-fable-5", 128000, true, true, true, false},
		{"anthropic.claude-opus-4-7-v1:0", 128000, true, true, true, false},
		{"anthropic.claude-sonnet-5", 128000, true, true, true, false},
		{"anthropic.claude-sonnet-4-6-v1:0", 128000, true, true, false, false},
		{"anthropic.claude-sonnet-4-5-20250929-v1:0", 64000, true, false, false, false},
		{"anthropic.claude-haiku-4-5-20251001-v1:0", 64000, true, false, false, false},
		{"anthropic.claude-opus-4-1-20250805-v1:0", 32000, true, false, false, false},
		{"anthropic.claude-sonnet-4-20250514-v1:0", 64000, false, false, false, false},
		{"anthropic.claude-opus-4-20250514-v1:0", 32000, false, false, false, false},
		{"anthropic.claude-3-haiku-20240307-v1:0", 4096, false, false, false, false},
		{"anthropic.claude-v2:1", 4096, false, false, false, false},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", 4096, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			caps := bedrockAnthropicModelCapabilities(tt.modelID)
			if caps.MaxOutputTokens != tt.maxOutputTokens {
				t.Errorf("MaxOutputTokens = %d, want %d", caps.MaxOutputTokens, tt.maxOutputTokens)
			}
			if caps.SupportsStructuredOutput != tt.supportsStructuredOutput {
				t.Errorf("SupportsStructuredOutput = %v, want %v", caps.SupportsStructuredOutput, tt.supportsStructuredOutput)
			}
			if caps.SupportsAdaptiveThinking != tt.supportsAdaptiveThinking {
				t.Errorf("SupportsAdaptiveThinking = %v, want %v", caps.SupportsAdaptiveThinking, tt.supportsAdaptiveThinking)
			}
			if caps.RejectsSamplingParams != tt.rejectsSamplingParams {
				t.Errorf("RejectsSamplingParams = %v, want %v", caps.RejectsSamplingParams, tt.rejectsSamplingParams)
			}
			if caps.RejectsForcedToolUse != tt.rejectsForcedToolUse {
				t.Errorf("RejectsForcedToolUse = %v, want %v", caps.RejectsForcedToolUse, tt.rejectsForcedToolUse)
			}
		})
	}
}

// TestBedrockRejectsSamplingParametersDropsFields verifies that models with
// rejectsSamplingParameters=true drop temperature/topK/topP with warnings.
func TestBedrockRejectsSamplingParametersDropsFields(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-opus-5")
	temp := 0.5
	topP := 0.9
	topK := 40
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temp,
		TopP:        &topP,
		TopK:        &topK,
	})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	inferenceConfig, _ := args.Body["inferenceConfig"].(map[string]interface{})
	if _, ok := inferenceConfig["temperature"]; ok {
		t.Errorf("expected temperature to be dropped, got %v", inferenceConfig["temperature"])
	}
	if _, ok := inferenceConfig["topP"]; ok {
		t.Errorf("expected topP to be dropped")
	}
	if _, ok := inferenceConfig["topK"]; ok {
		t.Errorf("expected topK to be dropped")
	}
	wantWarnings := map[string]bool{"temperature": false, "topP": false, "topK": false}
	for _, w := range args.Warnings {
		if _, ok := wantWarnings[w.Feature]; ok {
			wantWarnings[w.Feature] = true
		}
	}
	for feature, seen := range wantWarnings {
		if !seen {
			t.Errorf("expected unsupported warning for %s", feature)
		}
	}
}
