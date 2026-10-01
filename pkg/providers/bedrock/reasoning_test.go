package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
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

// TestBedrockTaskBudgetForwardsToOutputConfig verifies that
// providerOptions.anthropic.taskBudget is forwarded to
// additionalModelRequestFields.output_config.task_budget for Anthropic
// models, using the same wire shape as the direct Anthropic provider
// (pkg/providers/anthropic/request.go). Not present in TS's amazon-bedrock-
// chat-language-model.ts today (verified against ai@7.0.113); this is an
// additive parity feature gated entirely behind an explicit provider option,
// so it cannot change behavior for existing callers.
func TestBedrockTaskBudgetForwardsToOutputConfig(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-opus-5")
	remaining := 500
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"taskBudget": map[string]interface{}{
					"type":      "conversation",
					"total":     10000,
					"remaining": remaining,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	outputConfig, _ := fields["output_config"].(map[string]interface{})
	taskBudget, ok := outputConfig["task_budget"].(map[string]interface{})
	if !ok {
		t.Fatalf("output_config.task_budget = %#v, want a map", outputConfig["task_budget"])
	}
	if taskBudget["type"] != "conversation" || taskBudget["total"] != 10000 || taskBudget["remaining"] != 500 {
		t.Fatalf("task_budget = %#v", taskBudget)
	}

	// Regression: the underlying Anthropic model gates output_config.
	// task_budget on the task-budgets-2026-03-13 beta (pkg/providers/
	// anthropic/request.go, anthropic-language-model.ts:984-986); Bedrock
	// forwards taskBudget to that same backend via additionalModelRequestFields.
	// anthropic_beta and must add the beta itself, or the request is rejected.
	betas, ok := fields["anthropic_beta"].([]interface{})
	if !ok {
		t.Fatalf("anthropic_beta = %#v, want a []interface{} containing task-budgets-2026-03-13", fields["anthropic_beta"])
	}
	found := false
	for _, b := range betas {
		if b == "task-budgets-2026-03-13" {
			found = true
		}
	}
	if !found {
		t.Fatalf("anthropic_beta = %#v, want task-budgets-2026-03-13", betas)
	}
}

// TestBedrockTaskBudgetMergesWithExistingAnthropicBeta verifies that
// taskBudget's automatic beta add merges with (rather than overwrites) a
// caller-supplied providerOptions.amazonBedrock.anthropicBeta list, and does
// not add a duplicate if the caller already included it.
func TestBedrockTaskBudgetMergesWithExistingAnthropicBeta(t *testing.T) {
	model := newBedrockModelWithID("anthropic.claude-opus-5")
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{
				"anthropicBeta": []interface{}{"some-other-beta-2026-01-01"},
			},
			"anthropic": map[string]interface{}{
				"taskBudget": map[string]interface{}{"type": "conversation", "total": 10000},
			},
		},
	})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	betas, ok := fields["anthropic_beta"].([]interface{})
	if !ok {
		t.Fatalf("anthropic_beta = %#v, want a []interface{}", fields["anthropic_beta"])
	}
	if len(betas) != 2 {
		t.Fatalf("anthropic_beta = %#v, want exactly 2 entries (existing + task-budgets)", betas)
	}
	hasExisting, hasTaskBudget := false, false
	for _, b := range betas {
		switch b {
		case "some-other-beta-2026-01-01":
			hasExisting = true
		case "task-budgets-2026-03-13":
			hasTaskBudget = true
		}
	}
	if !hasExisting || !hasTaskBudget {
		t.Fatalf("anthropic_beta = %#v, want both the existing and task-budgets betas", betas)
	}
}

// TestBedrockTaskBudgetIgnoredForNonAnthropicModels verifies taskBudget is
// only forwarded for Anthropic models on Bedrock.
func TestBedrockTaskBudgetIgnoredForNonAnthropicModels(t *testing.T) {
	model := newBedrockModelWithID("us.amazon.nova-pro-v1:0")
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"taskBudget": map[string]interface{}{"type": "conversation", "total": 10000},
			},
		},
	})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	if outputConfig, ok := fields["output_config"].(map[string]interface{}); ok {
		if _, ok := outputConfig["task_budget"]; ok {
			t.Fatalf("expected no task_budget for a non-Anthropic model, got %#v", outputConfig)
		}
	}
}

// TestBedrockNonAnthropicReasoningEffort verifies that non-Anthropic models
// with known portable-reasoning support (Nova 2 Lite) map reasoning to
// additionalModelRequestFields.reasoningConfig.maxReasoningEffort (plus
// type:"enabled"), and that ReasoningNone adds no reasoning fields at all
// (mirrors TS: the non-Anthropic branch of resolveAmazonBedrockReasoningConfig
// only runs when reasoning !== 'none'). Ports TS "should map portable
// reasoning for Nova 2".
func TestBedrockNonAnthropicReasoningEffort(t *testing.T) {
	model := newBedrockModelWithID(ModelAmazonNova2LiteV1)

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
			if rc["type"] != "enabled" {
				t.Fatalf("reasoningConfig.type = %v, want enabled", rc["type"])
			}
			if rc["maxReasoningEffort"] != tt.want {
				t.Fatalf("maxReasoningEffort = %v, want %v", rc["maxReasoningEffort"], tt.want)
			}
		})
	}
}

// TestBedrockReasoningIgnoredForModelsWithoutKnownSupport ports TS "should
// ignore portable reasoning for models without known reasoning support":
// non-Anthropic models that are neither OpenAI models nor Nova 2 Lite get an
// "unsupported" warning and no reasoningConfig is derived.
func TestBedrockReasoningIgnoredForModelsWithoutKnownSupport(t *testing.T) {
	model := newBedrockModelWithID(ModelAmazonNovaMicroV1)
	level := types.ReasoningHigh
	args, err := model.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	if rc, ok := fields["reasoningConfig"]; ok {
		t.Fatalf("expected no reasoningConfig, got %v", rc)
	}
	found := false
	for _, w := range args.Warnings {
		if w.Type == "unsupported" && w.Feature == "reasoning" &&
			w.Details == "Portable reasoning is not supported for this model and will be ignored. If the model supports a provider-specific reasoning configuration, use providerOptions.amazonBedrock.reasoningConfig." {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported reasoning warning, got %#v", args.Warnings)
	}
}

// TestBedrockReasoningExplicitConfigForModelsWithoutKnownSupport ports TS
// "should forward explicit reasoningConfig for models without known
// reasoning support": an explicit providerOptions.amazonBedrock.reasoningConfig
// is still forwarded even when the model has no known portable-reasoning
// support.
func TestBedrockReasoningExplicitConfigForModelsWithoutKnownSupport(t *testing.T) {
	model := newBedrockModelWithID(ModelAmazonNovaMicroV1)
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{
				"reasoningConfig": map[string]interface{}{
					"type":               "enabled",
					"maxReasoningEffort": "high",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("getArgs error: %v", err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	rc, ok := fields["reasoningConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoningConfig, got none")
	}
	if rc["type"] != "enabled" || rc["maxReasoningEffort"] != "high" {
		t.Fatalf("reasoningConfig = %#v", rc)
	}
}

// TestBedrockUsesSharedAnthropicModelCapabilities verifies the Bedrock
// Converse client consumes the shared, exported
// anthropic.GetModelCapabilities (pkg/providers/anthropic/model_capabilities.go,
// WG-A1) rather than a bedrock-local duplicate of the capability table. The
// full table is exercised by anthropic's own tests
// (pkg/providers/anthropic/model_capabilities_test.go); this only spot-checks
// that a couple of representative models flow through correctly end-to-end.
func TestBedrockUsesSharedAnthropicModelCapabilities(t *testing.T) {
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
		{"anthropic.claude-sonnet-4-5-20250929-v1:0", 64000, true, false, false, false},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", 4096, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			caps := anthropic.GetModelCapabilities(tt.modelID)
			if caps.MaxOutputTokens != tt.maxOutputTokens {
				t.Errorf("MaxOutputTokens = %d, want %d", caps.MaxOutputTokens, tt.maxOutputTokens)
			}
			if caps.SupportsStructuredOutput != tt.supportsStructuredOutput {
				t.Errorf("SupportsStructuredOutput = %v, want %v", caps.SupportsStructuredOutput, tt.supportsStructuredOutput)
			}
			if caps.SupportsAdaptiveThinking != tt.supportsAdaptiveThinking {
				t.Errorf("SupportsAdaptiveThinking = %v, want %v", caps.SupportsAdaptiveThinking, tt.supportsAdaptiveThinking)
			}
			if caps.RejectsSamplingParameters != tt.rejectsSamplingParams {
				t.Errorf("RejectsSamplingParameters = %v, want %v", caps.RejectsSamplingParameters, tt.rejectsSamplingParams)
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
