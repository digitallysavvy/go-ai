package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestChatAudioTranscriptFallsBackToText ports 6d1f881: when message.content
// is empty, use message.audio.transcript as the text content.
func TestChatAudioTranscriptFallsBackToText(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4o-audio-preview")

	resp := openAIResponse{
		Choices: []struct {
			Index        int           `json:"index"`
			Message      openAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			{
				Message: openAIMessage{
					Role: "assistant",
					Audio: &struct {
						Transcript string `json:"transcript"`
					}{Transcript: "hello from audio"},
				},
				FinishReason: "stop",
			},
		},
	}
	result := m.convertResponse(resp)
	if result.Text != "hello from audio" {
		t.Fatalf("Text = %q, want audio transcript", result.Text)
	}
}

// TestChatContentPreferredOverAudioTranscript ensures message.content still
// wins when both are present.
func TestChatContentPreferredOverAudioTranscript(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4o-audio-preview")

	resp := openAIResponse{
		Choices: []struct {
			Index        int           `json:"index"`
			Message      openAIMessage `json:"message"`
			FinishReason string        `json:"finish_reason"`
		}{
			{
				Message: openAIMessage{
					Role:    "assistant",
					Content: "text content",
					Audio: &struct {
						Transcript string `json:"transcript"`
					}{Transcript: "hello from audio"},
				},
				FinishReason: "stop",
			},
		},
	}
	result := m.convertResponse(resp)
	if result.Text != "text content" {
		t.Fatalf("Text = %q, want text content", result.Text)
	}
}

// TestChatCacheWriteTokensInUsageDetails covers the b2b1bb9 usage half:
// prompt_tokens_details.cache_write_tokens surfaces on InputDetails, and
// noCache excludes both cache read and cache write tokens.
func TestChatCacheWriteTokensInUsageDetails(t *testing.T) {
	cached := 10
	cacheWrite := 5
	usage := openAIUsage{
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		PromptTokensDetails: &struct {
			CachedTokens     *int `json:"cached_tokens,omitempty"`
			CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
			AudioTokens      *int `json:"audio_tokens,omitempty"`
			TextTokens       *int `json:"text_tokens,omitempty"`
			ImageTokens      *int `json:"image_tokens,omitempty"`
		}{CachedTokens: &cached, CacheWriteTokens: &cacheWrite},
	}
	result := convertOpenAIUsage(usage)
	if result.InputDetails == nil {
		t.Fatal("InputDetails is nil")
	}
	if result.InputDetails.CacheWriteTokens == nil || *result.InputDetails.CacheWriteTokens != 5 {
		t.Fatalf("CacheWriteTokens = %v, want 5", result.InputDetails.CacheWriteTokens)
	}
	if result.InputDetails.NoCacheTokens == nil || *result.InputDetails.NoCacheTokens != 85 {
		t.Fatalf("NoCacheTokens = %v, want 85 (100-10-5)", result.InputDetails.NoCacheTokens)
	}
}

// TestChatServiceTierForwardedWhenSupported covers 17d3436/4cd4548: an
// unrestricted (non-flex/priority/fast) tier, and a supported model, are
// forwarded as service_tier.
func TestChatServiceTierForwardedWhenSupported(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-5") // supports both flex and priority

	for _, tier := range []string{"flex", "priority", "fast", "default", "auto"} {
		body, warnings, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"serviceTier": tier}},
		}, false)
		if body["service_tier"] != tier {
			t.Fatalf("tier=%s: service_tier = %#v, want %s (warnings=%#v)", tier, body["service_tier"], tier, warnings)
		}
		for _, w := range warnings {
			if w.Feature == "serviceTier" {
				t.Fatalf("tier=%s: unexpected warning %#v", tier, w)
			}
		}
	}
}

// TestChatServiceTierWarnsWhenUnsupported covers the gating half of
// 17d3436/4cd4548: flex requires supportsFlexProcessing, priority/fast
// require supportsPriorityProcessing.
func TestChatServiceTierWarnsWhenUnsupported(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4.1") // no flex, has priority

	body, warnings, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"serviceTier": "flex"}},
	}, false)
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("service_tier should be omitted, got %#v", body["service_tier"])
	}
	var found bool
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "serviceTier" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported serviceTier warning, got %#v", warnings)
	}

	// gpt-5-nano has neither priority nor flex (isGptNanoModel).
	mNano := NewLanguageModel(p, "gpt-5-nano")
	body, warnings, _ = mNano.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"serviceTier": "priority"}},
	}, false)
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("service_tier should be omitted for gpt-5-nano, got %#v", body["service_tier"])
	}
	found = false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "serviceTier" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported serviceTier warning for gpt-5-nano priority, got %#v", warnings)
	}
}

// TestChatReasoningSummaryWarnsOnChatModel ports TS commit 5b8e63bad8
// (#21178): providerOptions.openai.reasoningSummary is a Responses API
// option the chat options schema has no field for, so it would otherwise be
// silently dropped. The chat model must warn instead of staying silent.
func TestChatReasoningSummaryWarnsOnChatModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "o3")

	_, warnings, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningSummary": "detailed"}},
	}, false)

	want := types.Warning{
		Type:    "unsupported",
		Feature: "reasoningSummary",
		Details: "reasoningSummary is only supported by the Responses API, not the Chat Completions API",
	}
	var found bool
	for _, w := range warnings {
		if w == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want to contain %#v", warnings, want)
	}

	// No reasoningSummary option set: no warning.
	_, warnings, _ = m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	}, false)
	for _, w := range warnings {
		if w.Feature == "reasoningSummary" {
			t.Fatalf("unexpected reasoningSummary warning when option unset: %#v", warnings)
		}
	}
}

// TestChatPromptCacheOptionsForwarded covers the b2b1bb9 chat half:
// providerOptions.openai.promptCacheOptions forwards to prompt_cache_options.
func TestChatPromptCacheOptionsForwarded(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-5.6")

	opts := map[string]interface{}{"mode": "manual", "ttl": "24h"}
	body, _, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheOptions": opts}},
	}, false)
	got, ok := body["prompt_cache_options"].(map[string]interface{})
	if !ok || got["mode"] != "manual" || got["ttl"] != "24h" {
		t.Fatalf("prompt_cache_options = %#v", body["prompt_cache_options"])
	}
}

// TestChatPromptCacheRetentionDroppedForGpt6 covers b2b1bb9: promptCacheRetention
// is unsupported (with a warning) on GPT-6+ models, but still forwarded on
// pre-GPT-6 models.
func TestChatPromptCacheRetentionDroppedForGpt6(t *testing.T) {
	p := New(Config{APIKey: "k"})

	m6 := NewLanguageModel(p, "gpt-6-astra")
	body, warnings, _ := m6.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheRetention": "24h"}},
	}, false)
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention should be dropped for GPT-6+, got %#v", body["prompt_cache_retention"])
	}
	var found bool
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "promptCacheRetention" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported promptCacheRetention warning, got %#v", warnings)
	}

	m5 := NewLanguageModel(p, "gpt-5.1")
	body, warnings, _ = m5.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheRetention": "24h"}},
	}, false)
	if body["prompt_cache_retention"] != "24h" {
		t.Fatalf("prompt_cache_retention = %#v, want 24h for pre-GPT-6 model", body["prompt_cache_retention"])
	}
	for _, w := range warnings {
		if w.Feature == "promptCacheRetention" {
			t.Fatalf("unexpected warning for pre-GPT-6 model: %#v", w)
		}
	}
}

// TestChatReasoningEffortMaxAcceptedForGpt6 covers b2b1bb9: "max" effort is a
// valid value for GPT-6+ models (and gpt-6-sol/luna also accept "none").
func TestChatReasoningEffortMaxAcceptedForGpt6(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-6-astra")

	body, warnings, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "max"}},
	}, false)
	if body["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %#v, want max", body["reasoning_effort"])
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}

// TestChatReasoningEffortRejectedWhenUnsupportedForGpt6 covers 34c53c0: an
// effort not in SupportedReasoningEfforts is dropped with a warning.
func TestChatReasoningEffortRejectedWhenUnsupportedForGpt6(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-6-astra")

	body, warnings, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "disabled"}},
	}, false)
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort should be dropped, got %#v", body["reasoning_effort"])
	}
	var found bool
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "reasoningEffort" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unsupported reasoningEffort warning, got %#v", warnings)
	}
}

// TestChatReasoningEffortProviderOptionOverridesTopLevel covers TS's
// `openaiOptions.reasoningEffort ?? reasoning` precedence.
func TestChatReasoningEffortProviderOptionOverridesTopLevel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "o3")

	reasoning := types.ReasoningLow
	body, _, _ := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       &reasoning,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "high"}},
	}, false)
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high (provider option should win)", body["reasoning_effort"])
	}
}

// TestChatResponseFormatSchemaNormalized covers wiring NormalizeOpenAIJSONSchema
// (d5e3024/411b3f2) into the chat response_format path.
func TestChatResponseFormatSchemaNormalized(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4.1")

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"email": map[string]interface{}{
				"type":    "string",
				"pattern": `^(?!\.)(?!.*\.\.).+@.+$`,
			},
		},
	}
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: schema,
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rf, ok := body["response_format"].(map[string]interface{})
	if !ok || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %#v", body["response_format"])
	}
	jsonSchema := rf["json_schema"].(map[string]interface{})
	gotSchema := jsonSchema["schema"].(map[string]interface{})
	email := gotSchema["properties"].(map[string]interface{})["email"].(map[string]interface{})
	if _, ok := email["pattern"]; ok {
		t.Fatalf("pattern should have been stripped, got %#v", email)
	}
	var found bool
	for _, w := range warnings {
		if w.Type == "compatibility" && w.Feature == "JSON Schema pattern with regex lookaround" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected compatibility warning, got %#v", warnings)
	}
}

// TestChatResponseFormatSchemaNormalizationErrorPropagates covers the
// UnsupportedFunctionalityError path (a non-string propertyNames sub-schema)
// surfacing from DoGenerate/DoStream via buildRequestBodyWithWarnings.
func TestChatResponseFormatSchemaNormalizationErrorPropagates(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4.1")

	schema := map[string]interface{}{
		"type":          "object",
		"propertyNames": map[string]interface{}{"type": "number"},
	}
	_, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: schema,
		},
	}, false)
	if err == nil {
		t.Fatal("expected an UnsupportedFunctionalityError")
	}
}

// TestChatFunctionToolSchemaNormalized covers d5e3024/411b3f2 applied to
// function tool parameters (mirrors TS prepareChatTools).
func TestChatFunctionToolSchemaNormalized(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4.1")

	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{
				Name: "lookup",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"extra": map[string]interface{}{
							"type": "object",
							"propertyNames": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tools := body["tools"].([]map[string]interface{})
	fn := tools[0]["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})
	extra := params["properties"].(map[string]interface{})["extra"].(map[string]interface{})
	if _, ok := extra["propertyNames"]; ok {
		t.Fatalf("propertyNames should have been stripped, got %#v", extra)
	}
	var found bool
	for _, w := range warnings {
		if w.Feature == "JSON Schema propertyNames" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected propertyNames compatibility warning, got %#v", warnings)
	}
}

// TestChatSystemMessageModeUsesCapabilities covers 34c53c0's SystemMessageMode
// applied to the chat model's system role selection.
func TestChatSystemMessageModeUsesCapabilities(t *testing.T) {
	p := New(Config{APIKey: "k"})

	reasoningModel := NewLanguageModel(p, "gpt-6-astra")
	body := reasoningModel.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{System: "sys", Text: "hi"},
	}, false)
	msgs := body["messages"].([]map[string]interface{})
	if msgs[0]["role"] != "developer" {
		t.Fatalf("role = %v, want developer for gpt-6-astra", msgs[0]["role"])
	}

	chatModel := NewLanguageModel(p, "gpt-4.1")
	body = chatModel.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{System: "sys", Text: "hi"},
	}, false)
	msgs = body["messages"].([]map[string]interface{})
	if msgs[0]["role"] != "system" {
		t.Fatalf("role = %v, want system for gpt-4.1", msgs[0]["role"])
	}
}

// TestChatReasoningModelStripsSamplingParamsAndWarns ports TS's "should clear
// out temperature, top_p, frequency_penalty, presence_penalty and return
// warnings" (o4-mini has no supportsNonReasoningParameters, so this always
// strips regardless of reasoning effort).
func TestChatReasoningModelStripsSamplingParamsAndWarns(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "o4-mini")

	temp := 0.5
	topP := 0.7
	freq := 0.2
	pres := 0.3
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "Hello"},
		Temperature:      &temp,
		TopP:             &topP,
		FrequencyPenalty: &freq,
		PresencePenalty:  &pres,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	for _, key := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty"} {
		if _, ok := body[key]; ok {
			t.Fatalf("body[%q] should be stripped, got %#v", key, body[key])
		}
	}
	wantFeatures := []string{"temperature", "topP", "frequencyPenalty", "presencePenalty"}
	if len(warnings) != len(wantFeatures) {
		t.Fatalf("warnings = %#v, want %d entries", warnings, len(wantFeatures))
	}
	for i, feat := range wantFeatures {
		if warnings[i].Type != "unsupported" || warnings[i].Feature != feat {
			t.Fatalf("warnings[%d] = %#v, want unsupported/%s", i, warnings[i], feat)
		}
	}
}

// TestChatReasoningModelConvertsMaxTokensToMaxCompletionTokens ports TS's
// "should convert maxOutputTokens to max_completion_tokens".
func TestChatReasoningModelConvertsMaxTokensToMaxCompletionTokens(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "o4-mini")

	maxTokens := 1000
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Hello"},
		MaxTokens: &maxTokens,
	}, false)
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("max_tokens should be removed, got %#v", body["max_tokens"])
	}
	if body["max_completion_tokens"] != 1000 {
		t.Fatalf("max_completion_tokens = %#v, want 1000", body["max_completion_tokens"])
	}
}

// TestChatGpt51AllowsTemperatureWhenReasoningNone ports TS's "should allow
// temperature when top-level reasoning is none on gpt-5.1".
func TestChatGpt51AllowsTemperatureWhenReasoningNone(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-5.1")

	temp := 0.5
	none := types.ReasoningNone
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "Hello"},
		Reasoning:   &none,
		Temperature: &temp,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	if body["reasoning_effort"] != "none" {
		t.Fatalf("reasoning_effort = %#v, want none", body["reasoning_effort"])
	}
	if body["temperature"] != 0.5 {
		t.Fatalf("temperature = %#v, want 0.5 (gpt-5.1 allows it when effort is none)", body["temperature"])
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
}

// TestChatO4MiniStillClearsTemperatureWhenReasoningNone ports TS's "should
// still clear temperature when top-level reasoning is none on o4-mini" --
// unlike gpt-5.1, o4-mini does not support non-reasoning parameters at all.
func TestChatO4MiniStillClearsTemperatureWhenReasoningNone(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "o4-mini")

	temp := 0.5
	none := types.ReasoningNone
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "Hello"},
		Reasoning:   &none,
		Temperature: &temp,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	if body["reasoning_effort"] != "none" {
		t.Fatalf("reasoning_effort = %#v, want none", body["reasoning_effort"])
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("temperature should be stripped for o4-mini, got %#v", body["temperature"])
	}
	if len(warnings) != 1 || warnings[0].Feature != "temperature" {
		t.Fatalf("warnings = %#v, want a single temperature warning", warnings)
	}
}

// TestChatGpt6SolLunaPreservesNoneReasoningEffort ports TS's "should preserve
// disabled reasoning for gpt-6-sol/gpt-6-luna".
func TestChatGpt6SolLunaPreservesNoneReasoningEffort(t *testing.T) {
	p := New(Config{APIKey: "k"})
	for _, id := range []string{"gpt-6-sol", "gpt-6-luna"} {
		m := NewLanguageModel(p, id)
		body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "Hello"},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "none"}},
		}, false)
		if err != nil {
			t.Fatalf("%s: buildRequestBodyWithWarnings: %v", id, err)
		}
		if body["reasoning_effort"] != "none" {
			t.Fatalf("%s: reasoning_effort = %#v, want none", id, body["reasoning_effort"])
		}
		if len(warnings) != 0 {
			t.Fatalf("%s: warnings = %#v, want none", id, warnings)
		}
	}
}

// TestChatGpt6StripsSamplingAndLogprobSettings ports TS's "should strip
// sampling and logprob settings for GPT-6 models".
func TestChatGpt6StripsSamplingAndLogprobSettings(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-6-astra")

	temp := 0.5
	topP := 0.7
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "Hello"},
		Temperature: &temp,
		TopP:        &topP,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"reasoningEffort": "low",
			"logprobs":        5,
		}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	if body["reasoning_effort"] != "low" {
		t.Fatalf("reasoning_effort = %#v, want low", body["reasoning_effort"])
	}
	for _, key := range []string{"temperature", "top_p", "logprobs", "top_logprobs"} {
		if _, ok := body[key]; ok {
			t.Fatalf("body[%q] should be stripped, got %#v", key, body[key])
		}
	}
	if len(warnings) != 4 {
		t.Fatalf("warnings = %#v, want 4 entries", warnings)
	}
	if warnings[0].Feature != "temperature" || warnings[1].Feature != "topP" {
		t.Fatalf("warnings[0:2] = %#v", warnings[:2])
	}
	if warnings[2].Type != "other" || warnings[2].Message != "logprobs is not supported for reasoning models" {
		t.Fatalf("warnings[2] = %#v", warnings[2])
	}
	if warnings[3].Type != "other" || warnings[3].Message != "topLogprobs is not supported for reasoning models" {
		t.Fatalf("warnings[3] = %#v", warnings[3])
	}
}

// TestChatForceReasoningAppliesParameterCompatibilityRules ports TS's "should
// allow forcing reasoning behavior for unrecognized model IDs via
// providerOptions".
func TestChatForceReasoningAppliesParameterCompatibilityRules(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "stealth-reasoning-model")

	temp := 0.5
	topP := 0.7
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		Temperature:     &temp,
		TopP:            &topP,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"forceReasoning": true}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("temperature should be stripped, got %#v", body["temperature"])
	}
	if _, ok := body["top_p"]; ok {
		t.Fatalf("top_p should be stripped, got %#v", body["top_p"])
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v, want 2 entries", warnings)
	}
}

// TestChatForceReasoningDefaultsSystemMessageModeToDeveloper ports TS's
// "should default systemMessageMode to developer when forcing reasoning".
func TestChatForceReasoningDefaultsSystemMessageModeToDeveloper(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "stealth-reasoning-model")

	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "You are a helpful assistant.",
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
			},
		},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"forceReasoning": true}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	msgs := body["messages"].([]map[string]interface{})
	if msgs[0]["role"] != "developer" {
		t.Fatalf("role = %v, want developer", msgs[0]["role"])
	}
}

// TestChatSystemMessageModeOverrideViaProviderOptions ports TS's "should
// allow overriding systemMessageMode via providerOptions" and "should use
// default systemMessageMode when not overridden".
func TestChatSystemMessageModeOverrideViaProviderOptions(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4o")
	promptOpts := types.Prompt{
		System: "You are a helpful assistant.",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		},
	}

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          promptOpts,
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"systemMessageMode": "developer"}},
	}, false)
	msgs := body["messages"].([]map[string]interface{})
	if msgs[0]["role"] != "developer" {
		t.Fatalf("role = %v, want developer (overridden)", msgs[0]["role"])
	}

	body = m.buildRequestBody(&provider.GenerateOptions{Prompt: promptOpts}, false)
	msgs = body["messages"].([]map[string]interface{})
	if msgs[0]["role"] != "system" {
		t.Fatalf("role = %v, want system (default for gpt-4o)", msgs[0]["role"])
	}
}

// TestChatSystemMessageModeRemoveDropsSystemMessageAndWarns ports TS
// convertToOpenAIChatMessages' 'remove' branch: system messages are removed
// entirely, with a warning, when systemMessageMode resolves to 'remove'.
func TestChatSystemMessageModeRemoveDropsSystemMessageAndWarns(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-4o")

	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "You are a helpful assistant.",
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
			},
		},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"systemMessageMode": "remove"}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings: %v", err)
	}
	msgs := body["messages"].([]map[string]interface{})
	if len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Fatalf("messages = %#v, want only the user message", msgs)
	}
	if len(warnings) != 1 || warnings[0].Message != "system messages are removed for this model" {
		t.Fatalf("warnings = %#v, want a single 'system messages are removed' warning", warnings)
	}
}

// TestChatSearchPreviewModelsStripTemperature ports TS's "should remove
// temperature setting for gpt-4o-search-preview/gpt-4o-mini-search-preview
// and add warning".
func TestChatSearchPreviewModelsStripTemperature(t *testing.T) {
	p := New(Config{APIKey: "k"})
	for _, id := range []string{
		"gpt-4o-search-preview",
		"gpt-4o-mini-search-preview",
		"gpt-4o-mini-search-preview-2025-03-11",
	} {
		m := NewLanguageModel(p, id)
		temp := 0.7
		body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:      types.Prompt{Text: "Hello"},
			Temperature: &temp,
		}, false)
		if err != nil {
			t.Fatalf("%s: buildRequestBodyWithWarnings: %v", id, err)
		}
		if _, ok := body["temperature"]; ok {
			t.Fatalf("%s: temperature should be stripped, got %#v", id, body["temperature"])
		}
		if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "temperature" ||
			warnings[0].Details != "temperature is not supported for the search preview models and has been removed." {
			t.Fatalf("%s: warnings = %#v", id, warnings)
		}
	}
}

// TestChatMaxCompletionTokensProviderOption ports TS's "should send
// max_completion_tokens extension setting".
func TestChatMaxCompletionTokensProviderOption(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-3.5-turbo")

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"maxCompletionTokens": 255}},
	}, false)
	if body["max_completion_tokens"] != 255 {
		t.Fatalf("max_completion_tokens = %#v, want 255", body["max_completion_tokens"])
	}
}

// TestChatSafetyIdentifierProviderOption ports TS's "should send
// safetyIdentifier extension value".
func TestChatSafetyIdentifierProviderOption(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewLanguageModel(p, "gpt-3.5-turbo")

	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": "test-safety-identifier-123"}},
	}, false)
	if body["safety_identifier"] != "test-safety-identifier-123" {
		t.Fatalf("safety_identifier = %#v, want test-safety-identifier-123", body["safety_identifier"])
	}
}
