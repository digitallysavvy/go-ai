package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestDefaultSettingsMiddleware_AppliesDefaults(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	defaultTemp := 0.7
	defaultMaxTokens := 1000
	defaults := &provider.GenerateOptions{
		Temperature: &defaultTemp,
		MaxTokens:   &defaultMaxTokens,
	}

	middleware := DefaultSettingsMiddleware(defaults)
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	// Call without any overrides
	_, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that defaults were applied
	if len(model.GenerateCalls) != 1 {
		t.Fatal("expected 1 generate call")
	}
	receivedOpts := model.GenerateCalls[0]
	if receivedOpts.Temperature == nil || *receivedOpts.Temperature != defaultTemp {
		t.Errorf("expected temperature %f, got %v", defaultTemp, receivedOpts.Temperature)
	}
	if receivedOpts.MaxTokens == nil || *receivedOpts.MaxTokens != defaultMaxTokens {
		t.Errorf("expected maxTokens %d, got %v", defaultMaxTokens, receivedOpts.MaxTokens)
	}
}

func TestDefaultSettingsMiddleware_OverridesDefaults(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{}

	defaultTemp := 0.7
	defaults := &provider.GenerateOptions{
		Temperature: &defaultTemp,
	}

	middleware := DefaultSettingsMiddleware(defaults)
	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)

	// Override the temperature
	overrideTemp := 0.9
	_, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{
		Temperature: &overrideTemp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that override was applied
	receivedOpts := model.GenerateCalls[0]
	if receivedOpts.Temperature == nil || *receivedOpts.Temperature != overrideTemp {
		t.Errorf("expected temperature %f, got %v", overrideTemp, receivedOpts.Temperature)
	}
}

func TestMergeGenerateOptions_NilDefaults(t *testing.T) {
	t.Parallel()

	temp := 0.5
	overrides := &provider.GenerateOptions{
		Temperature: &temp,
	}

	result := mergeGenerateOptions(nil, overrides)

	if result != overrides {
		t.Error("expected overrides to be returned when defaults is nil")
	}
}

func TestMergeGenerateOptions_NilOverrides(t *testing.T) {
	t.Parallel()

	temp := 0.5
	defaults := &provider.GenerateOptions{
		Temperature: &temp,
	}

	result := mergeGenerateOptions(defaults, nil)

	if result != defaults {
		t.Error("expected defaults to be returned when overrides is nil")
	}
}

func TestMergeGenerateOptions_AllFields(t *testing.T) {
	t.Parallel()

	defaultTemp := 0.5
	defaultTopP := 0.9
	defaultMaxTokens := 100
	defaultTopK := 50
	defaultPresence := 0.1
	defaultFrequency := 0.2
	defaultSeed := 42
	defaultMaxSteps := 5

	defaults := &provider.GenerateOptions{
		Temperature:      &defaultTemp,
		TopP:             &defaultTopP,
		MaxTokens:        &defaultMaxTokens,
		TopK:             &defaultTopK,
		PresencePenalty:  &defaultPresence,
		FrequencyPenalty: &defaultFrequency,
		Seed:             &defaultSeed,
		StopSequences:    []string{"stop1"},
		MaxSteps:         &defaultMaxSteps,
		Headers:          map[string]string{"X-Default": "value"},
		Tools: []types.Tool{
			{Name: "default_tool"},
		},
		ToolChoice: types.ToolChoice{Type: "auto"},
	}

	overrideTemp := 0.8
	overrideMaxTokens := 200

	overrides := &provider.GenerateOptions{
		Temperature: &overrideTemp,
		MaxTokens:   &overrideMaxTokens,
		Headers:     map[string]string{"X-Override": "value2"},
	}

	result := mergeGenerateOptions(defaults, overrides)

	// Should use override values
	if *result.Temperature != overrideTemp {
		t.Errorf("expected temperature %f, got %f", overrideTemp, *result.Temperature)
	}
	if *result.MaxTokens != overrideMaxTokens {
		t.Errorf("expected maxTokens %d, got %d", overrideMaxTokens, *result.MaxTokens)
	}

	// Should keep default values that weren't overridden
	if *result.TopP != defaultTopP {
		t.Errorf("expected topP %f, got %f", defaultTopP, *result.TopP)
	}
	if *result.TopK != defaultTopK {
		t.Errorf("expected topK %d, got %d", defaultTopK, *result.TopK)
	}
	if *result.PresencePenalty != defaultPresence {
		t.Errorf("expected presencePenalty %f, got %f", defaultPresence, *result.PresencePenalty)
	}
	if *result.FrequencyPenalty != defaultFrequency {
		t.Errorf("expected frequencyPenalty %f, got %f", defaultFrequency, *result.FrequencyPenalty)
	}
	if *result.Seed != defaultSeed {
		t.Errorf("expected seed %d, got %d", defaultSeed, *result.Seed)
	}
	if *result.MaxSteps != defaultMaxSteps {
		t.Errorf("expected maxSteps %d, got %d", defaultMaxSteps, *result.MaxSteps)
	}

	// Headers should be merged
	if result.Headers["X-Default"] != "value" {
		t.Error("expected default header to be preserved")
	}
	if result.Headers["X-Override"] != "value2" {
		t.Error("expected override header to be applied")
	}

	// Tools and ToolChoice should use defaults
	if len(result.Tools) != 1 || result.Tools[0].Name != "default_tool" {
		t.Error("expected default tools to be preserved")
	}
	if result.ToolChoice.Type != "auto" {
		t.Error("expected default tool choice to be preserved")
	}
}

func TestMergeGenerateOptions_ToolsOverride(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Tools: []types.Tool{
			{Name: "default_tool"},
		},
	}

	overrides := &provider.GenerateOptions{
		Tools: []types.Tool{
			{Name: "override_tool"},
		},
	}

	result := mergeGenerateOptions(defaults, overrides)

	if len(result.Tools) != 1 || result.Tools[0].Name != "override_tool" {
		t.Error("expected override tools to take precedence")
	}
}

func TestMergeGenerateOptions_ToolChoiceOverride(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		ToolChoice: types.ToolChoice{Type: "auto"},
	}

	overrides := &provider.GenerateOptions{
		ToolChoice: types.ToolChoice{Type: "required"},
	}

	result := mergeGenerateOptions(defaults, overrides)

	if result.ToolChoice.Type != "required" {
		t.Errorf("expected tool choice 'required', got %s", result.ToolChoice.Type)
	}
}

func TestMergeGenerateOptions_StopSequencesOverride(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		StopSequences: []string{"stop1", "stop2"},
	}

	overrides := &provider.GenerateOptions{
		StopSequences: []string{"stop3"},
	}

	result := mergeGenerateOptions(defaults, overrides)

	if len(result.StopSequences) != 1 || result.StopSequences[0] != "stop3" {
		t.Error("expected override stop sequences to take precedence")
	}
}

func TestMergeGenerateOptions_ResponseFormatOverride(t *testing.T) {
	t.Parallel()

	defaultFormat := &provider.ResponseFormat{Type: "json"}
	overrideFormat := &provider.ResponseFormat{Type: "text"}

	defaults := &provider.GenerateOptions{
		ResponseFormat: defaultFormat,
	}

	overrides := &provider.GenerateOptions{
		ResponseFormat: overrideFormat,
	}

	result := mergeGenerateOptions(defaults, overrides)

	if result.ResponseFormat == nil || result.ResponseFormat.Type != "text" {
		t.Error("expected override response format to take precedence")
	}
}

func TestMergeGenerateOptions_HeadersMerge(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Headers: map[string]string{
			"X-Default": "default-value",
			"X-Shared":  "default-shared",
		},
	}

	overrides := &provider.GenerateOptions{
		Headers: map[string]string{
			"X-Override": "override-value",
			"X-Shared":   "override-shared",
		},
	}

	result := mergeGenerateOptions(defaults, overrides)

	// Both headers should be present
	if result.Headers["X-Default"] != "default-value" {
		t.Error("expected default header to be preserved")
	}
	if result.Headers["X-Override"] != "override-value" {
		t.Error("expected override header to be added")
	}
	// Shared key should use override value
	if result.Headers["X-Shared"] != "override-shared" {
		t.Error("expected shared header to use override value")
	}
}

func TestDefaultSettingsMiddleware_SpecificationVersion(t *testing.T) {
	t.Parallel()

	middleware := DefaultSettingsMiddleware(&provider.GenerateOptions{})

	if middleware.SpecificationVersion != "v3" {
		t.Errorf("expected specification version 'v3', got %s", middleware.SpecificationVersion)
	}
}

func TestMergeGenerateOptions_EmptyOverrideHeaders(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Headers: map[string]string{
			"X-Default": "value",
		},
	}

	overrides := &provider.GenerateOptions{}

	result := mergeGenerateOptions(defaults, overrides)

	if result.Headers == nil || result.Headers["X-Default"] != "value" {
		t.Error("expected default headers to be preserved when override has no headers")
	}
}

func TestMergeGenerateOptions_Messages(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "default system"}}},
			},
		},
	}

	overrides := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "user message"}}},
			},
		},
	}

	result := mergeGenerateOptions(defaults, overrides)

	// Override messages should take precedence
	if len(result.Prompt.Messages) != 1 || result.Prompt.Messages[0].Role != types.RoleUser {
		t.Error("expected override messages to take precedence")
	}
}
