package middleware

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

// TestMergeGenerateOptions_PromptNeverFallsBackToDefault mirrors TS's
// defaultSettingsMiddleware: its `settings` parameter is typed as a
// `Partial<{...}>` that excludes `prompt` entirely (default-settings-
// middleware.ts), so a prompt/messages/system default can never reach
// mergeObjects(settings, params) in the first place. Go's
// provider.GenerateOptions has no such per-field partial type, so
// mergeGenerateOptions must instead simply never copy any Prompt.* field
// from defaults into the result, regardless of whether overrides supplies a
// prompt of its own. (Previously Go filled Prompt.Text/System/Messages from
// defaults whenever the corresponding override field was the zero value,
// which could silently inject a default prompt/system the caller never
// asked for.)
func TestMergeGenerateOptions_PromptNeverFallsBackToDefault(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Prompt: types.Prompt{
			System:   "default system prompt",
			Text:     "default text prompt",
			Messages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "default message"}}}},
		},
	}
	overrides := &provider.GenerateOptions{}

	result := mergeGenerateOptions(defaults, overrides)

	if result.Prompt.System != "" {
		t.Errorf("expected no default system prompt to be pulled in, got %q", result.Prompt.System)
	}
	if result.Prompt.Text != "" {
		t.Errorf("expected no default text prompt to be pulled in, got %q", result.Prompt.Text)
	}
	if result.Prompt.Messages != nil {
		t.Errorf("expected no default messages to be pulled in, got %#v", result.Prompt.Messages)
	}
}

// TestMergeGenerateOptions_PromptNeverFallsBackToDefault_CallerSuppliedOtherForm
// covers the case explicitly called out alongside the primary rule: even
// when the caller supplies some other prompt form (Text, not System), a
// defaults-side System must still not leak in.
func TestMergeGenerateOptions_PromptNeverFallsBackToDefault_CallerSuppliedOtherForm(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Prompt: types.Prompt{System: "default system prompt"},
	}
	overrides := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "caller text prompt"},
	}

	result := mergeGenerateOptions(defaults, overrides)

	if result.Prompt.Text != "caller text prompt" {
		t.Errorf("expected caller's text prompt to survive, got %q", result.Prompt.Text)
	}
	if result.Prompt.System != "" {
		t.Errorf("expected no default system prompt to be pulled in alongside caller's text prompt, got %q", result.Prompt.System)
	}
}

func TestMergeGenerateOptions_PromptSystemOverride(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		Prompt: types.Prompt{System: "default system prompt"},
	}
	overrides := &provider.GenerateOptions{
		Prompt: types.Prompt{System: "override system prompt"},
	}

	result := mergeGenerateOptions(defaults, overrides)

	if result.Prompt.System != "override system prompt" {
		t.Errorf("expected override system prompt to win, got %q", result.Prompt.System)
	}
}

func TestMergeGenerateOptions_ScalarCallOptionFieldsSurviveWithoutDefaults(t *testing.T) {
	t.Parallel()

	// Regression test for A2-2: fields not in the old hand-picked copy list
	// (ProviderOptions, Reasoning, SendReasoning, RuntimeContext,
	// ToolsContext, IncludeRawChunks, AllowSystemMessages/
	// AllowSystemInMessages, Telemetry) must survive from the caller's own
	// params even when no defaults are configured.
	reasoning := types.ReasoningLevel("high")
	sendReasoning := true

	overrides := &provider.GenerateOptions{
		AllowSystemMessages:   true,
		AllowSystemInMessages: true,
		IncludeRawChunks:      true,
		Reasoning:             &reasoning,
		SendReasoning:         &sendReasoning,
		RuntimeContext:        "runtime-value",
		ToolsContext:          map[string]interface{}{"tool1": "ctx"},
		ProviderOptions:       map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": "ephemeral"}},
		Telemetry:             &telemetry.Settings{FunctionID: "my-function"},
	}

	result := mergeGenerateOptions(&provider.GenerateOptions{}, overrides)

	if !result.AllowSystemMessages {
		t.Error("expected AllowSystemMessages to survive")
	}
	if !result.AllowSystemInMessages {
		t.Error("expected AllowSystemInMessages to survive")
	}
	if !result.IncludeRawChunks {
		t.Error("expected IncludeRawChunks to survive")
	}
	if result.Reasoning == nil || *result.Reasoning != reasoning {
		t.Error("expected Reasoning to survive")
	}
	if result.SendReasoning == nil || !*result.SendReasoning {
		t.Error("expected SendReasoning to survive")
	}
	if result.RuntimeContext != "runtime-value" {
		t.Error("expected RuntimeContext to survive")
	}
	if result.ToolsContext["tool1"] != "ctx" {
		t.Error("expected ToolsContext to survive")
	}
	if result.ProviderOptions == nil {
		t.Fatal("expected ProviderOptions to survive")
	}
	anthropic, ok := result.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok || anthropic["cacheControl"] != "ephemeral" {
		t.Error("expected ProviderOptions nested value to survive")
	}
	if result.Telemetry == nil || result.Telemetry.FunctionID != "my-function" {
		t.Error("expected Telemetry to survive")
	}
}

func TestMergeGenerateOptions_ProviderOptionsMergeFromDefaults(t *testing.T) {
	t.Parallel()

	defaults := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"cacheControl": map[string]interface{}{"type": "ephemeral"},
			},
		},
	}
	overrides := &provider.GenerateOptions{}

	result := mergeGenerateOptions(defaults, overrides)

	anthropic, ok := result.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok {
		t.Fatal("expected default provider options to survive when overrides has none")
	}
	cacheControl, ok := anthropic["cacheControl"].(map[string]interface{})
	if !ok || cacheControl["type"] != "ephemeral" {
		t.Error("expected nested default provider option value to survive")
	}
}

func TestMergeGenerateOptions_ProviderOptionsDeepMerge(t *testing.T) {
	t.Parallel()

	// Mirrors TS defaultSettingsMiddleware.test.ts "should handle nested
	// provider metadata objects correctly".
	defaults := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"tools": map[string]interface{}{
					"retrieval": map[string]interface{}{"enabled": true},
					"math":      map[string]interface{}{"enabled": true},
				},
			},
		},
	}
	overrides := &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"tools": map[string]interface{}{
					"retrieval": map[string]interface{}{"enabled": false},
					"code":      map[string]interface{}{"enabled": true},
				},
			},
		},
	}

	result := mergeGenerateOptions(defaults, overrides)

	anthropic := result.ProviderOptions["anthropic"].(map[string]interface{})
	tools := anthropic["tools"].(map[string]interface{})

	retrieval := tools["retrieval"].(map[string]interface{})
	if retrieval["enabled"] != false {
		t.Error("expected override retrieval.enabled=false to win")
	}
	math := tools["math"].(map[string]interface{})
	if math["enabled"] != true {
		t.Error("expected default math.enabled=true to be preserved")
	}
	code := tools["code"].(map[string]interface{})
	if code["enabled"] != true {
		t.Error("expected override-only code.enabled=true to be present")
	}
}

// TestGenerateOptions_AllFieldsCoveredByMerge enumerates provider.GenerateOptions'
// fields by reflection so that a field added in the future without updating
// this test (and mergeGenerateOptions) fails loudly instead of being
// silently dropped by the default-settings middleware, per A2-2.
func TestGenerateOptions_AllFieldsCoveredByMerge(t *testing.T) {
	t.Parallel()

	// Every field currently on provider.GenerateOptions must appear here.
	knownFields := map[string]bool{
		"Prompt":                true,
		"AllowSystemMessages":   true,
		"AllowSystemInMessages": true,
		"Temperature":           true,
		"MaxTokens":             true,
		"TopP":                  true,
		"TopK":                  true,
		"FrequencyPenalty":      true,
		"PresencePenalty":       true,
		"StopSequences":         true,
		"Tools":                 true,
		"IncludeRawChunks":      true,
		"ToolChoice":            true,
		"RuntimeContext":        true,
		"ToolsContext":          true,
		"ResponseFormat":        true,
		"Seed":                  true,
		"Headers":               true,
		"MaxSteps":              true,
		"Reasoning":             true,
		"SendReasoning":         true,
		"ProviderOptions":       true,
		"Telemetry":             true,
	}

	rt := reflect.TypeOf(provider.GenerateOptions{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if !knownFields[name] {
			t.Fatalf(
				"provider.GenerateOptions gained a new field %q that this test (and likely mergeGenerateOptions) doesn't account for; "+
					"update TestGenerateOptions_AllFieldsCoveredByMerge and mergeGenerateOptions in pkg/middleware/default_settings.go",
				name,
			)
		}
	}

	// Now verify every one of those fields actually survives a merge where
	// overrides sets every field and defaults sets none, i.e. nothing is
	// silently dropped by mergeGenerateOptions.
	reasoning := types.ReasoningLevel("high")
	sendReasoning := true
	temp := 1.5
	maxTokens := 111
	topP := 0.11
	topK := 22
	freq := 0.33
	pres := 0.44
	seed := 999
	maxSteps := 7

	overrides := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
			System:   "override-system",
			Text:     "override-text",
		},
		AllowSystemMessages:   true,
		AllowSystemInMessages: true,
		Temperature:           &temp,
		MaxTokens:             &maxTokens,
		TopP:                  &topP,
		TopK:                  &topK,
		FrequencyPenalty:      &freq,
		PresencePenalty:       &pres,
		StopSequences:         []string{"override-stop"},
		Tools:                 []types.Tool{{Name: "override-tool"}},
		IncludeRawChunks:      true,
		ToolChoice:            types.ToolChoice{Type: "required"},
		RuntimeContext:        "override-runtime",
		ToolsContext:          map[string]interface{}{"tool": "override"},
		ResponseFormat:        &provider.ResponseFormat{Type: "json"},
		Seed:                  &seed,
		Headers:               map[string]string{"X-Override": "1"},
		MaxSteps:              &maxSteps,
		Reasoning:             &reasoning,
		SendReasoning:         &sendReasoning,
		ProviderOptions:       map[string]interface{}{"anthropic": map[string]interface{}{"k": "override"}},
		Telemetry:             &telemetry.Settings{FunctionID: "override-fn"},
	}

	defaults := &provider.GenerateOptions{} // empty: overrides must win on every field

	result := mergeGenerateOptions(defaults, overrides)

	rv := reflect.ValueOf(*result)
	ov := reflect.ValueOf(*overrides)
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		got := rv.Field(i).Interface()
		want := ov.Field(i).Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("field %s was dropped or altered by mergeGenerateOptions: got %#v, want %#v", name, got, want)
		}
	}
}
