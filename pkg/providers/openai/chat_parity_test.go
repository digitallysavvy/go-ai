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
