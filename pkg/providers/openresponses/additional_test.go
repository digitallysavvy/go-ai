package openresponses

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type nopReadCloser struct {
	io.Reader
}

func (nopReadCloser) Close() error { return nil }

func TestProviderBasicsAndOptionsExtractors(t *testing.T) {
	p := New(Config{
		BaseURL: "http://localhost:1234/v1",
		APIKey:  "k",
		Headers: map[string]string{"X-Test": "1"},
	})
	if p.Name() != "open-responses" || p.Client() == nil {
		t.Fatalf("unexpected provider init: %+v", p)
	}
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected empty model id error")
	}
	if _, err := p.EmbeddingModel("x"); !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("embedding error = %v, want ErrModelNotFound", err)
	}
	if _, err := p.ImageModel("x"); !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("image error = %v, want ErrModelNotFound", err)
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected unsupported speech error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected unsupported transcription error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected unsupported reranking error")
	}

	opts := extractOpenResponsesProviderOptions(map[string]interface{}{
		"openai":         map[string]interface{}{"reasoningSummary": "detailed"},
		"open-responses": map[string]interface{}{"reasoningSummary": "legacy"},
	}, "open-responses")
	if opts.ReasoningSummary != "detailed" {
		t.Fatalf("provider options parse failed: %+v", opts)
	}
}

func TestConvertToolsChoicesAndUsage(t *testing.T) {
	tools, encodedProviderTools, toolWarnings := convertToolsToOpenResponses([]types.Tool{
		{Name: "weather", Description: "lookup", Parameters: map[string]interface{}{"type": "object"}, Strict: types.BoolPtr(true)},
	}, nil, "")
	if len(tools) != 1 {
		t.Fatalf("tools conversion failed: %+v", tools)
	}
	ft, ok := tools[0].(FunctionTool)
	if !ok || ft.Name != "weather" || ft.Strict == nil || !*ft.Strict {
		t.Fatalf("tools conversion failed: %+v", tools)
	}
	if len(toolWarnings) != 0 {
		t.Fatalf("unexpected tool warnings: %+v", toolWarnings)
	}
	if len(encodedProviderTools) != 0 {
		t.Fatalf("unexpected encoded provider tools: %+v", encodedProviderTools)
	}

	providerTools, providerEncodedTools, providerToolWarnings := convertToolsToOpenResponses([]types.Tool{
		{Name: "search", Type: "provider", ProviderID: "openai.web_search"},
	}, nil, "")
	if len(providerTools) != 0 {
		t.Fatalf("provider-defined tools should be skipped: %+v", providerTools)
	}
	if len(providerEncodedTools) != 0 {
		t.Fatalf("unexpected encoded provider tools: %+v", providerEncodedTools)
	}
	assertUnsupportedWarning(t, providerToolWarnings, "provider-defined tool openai.web_search")

	if got, _ := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "auto"}, nil); got != "auto" {
		t.Fatalf("auto choice = %#v", got)
	}
	if got, _ := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "required"}, nil); got != "required" {
		t.Fatalf("required choice = %#v", got)
	}
	if got, _ := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "none"}, nil); got != "none" {
		t.Fatalf("none choice = %#v", got)
	}
	toolChoice, _ := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "tool", ToolName: "weather"}, nil)
	choiceMap, ok := toolChoice.(map[string]interface{})
	if !ok || choiceMap["name"] != "weather" {
		t.Fatalf("tool choice conversion failed: %#v", toolChoice)
	}

	usage := convertOpenResponsesUsage(&Usage{
		InputTokens:         10,
		OutputTokens:        6,
		TotalTokens:         16,
		InputTokensDetails:  &InputTokensDetails{CachedTokens: 4},
		OutputTokensDetails: &OutputTokensDetails{ReasoningTokens: 2},
	})
	if usage.InputDetails == nil || usage.OutputDetails == nil {
		t.Fatalf("expected detailed usage conversion, got %+v", usage)
	}
}

func TestBuildRequestBodyWarningsAndReasoningMapping(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5")

	high := types.ReasoningHigh
	topK := 10
	seed := 42
	frequencyPenalty := 0.2
	presencePenalty := 0.3
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		Reasoning:        &high,
		TopK:             &topK,
		Seed:             &seed,
		StopSequences:    []string{},
		FrequencyPenalty: &frequencyPenalty,
		PresencePenalty:  &presencePenalty,
		ProviderOptions:  map[string]interface{}{"openai": map[string]interface{}{"reasoningSummary": "concise"}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if _, ok := body["stream"]; ok {
		t.Fatalf("non-stream request should omit stream field like TS baseArgs: %#v", body["stream"])
	}
	if _, ok := body["tools"]; ok {
		t.Fatalf("request should omit tools field when no tools are configured like TS, got %#v", body["tools"])
	}
	if len(warnings) < 5 {
		t.Fatalf("expected warnings for unsupported settings, got %+v", warnings)
	}
	warningFeatures := map[string]bool{}
	for _, warning := range warnings {
		if warning.Type == "unsupported" {
			warningFeatures[warning.Feature] = true
		}
	}
	for _, feature := range []string{"frequencyPenalty", "presencePenalty", "stopSequences", "topK", "seed"} {
		if !warningFeatures[feature] {
			t.Fatalf("missing unsupported warning for %s: %+v", feature, warnings)
		}
	}
	for _, key := range []string{"frequency_penalty", "presence_penalty"} {
		if _, ok := body[key]; ok {
			t.Fatalf("%s should be omitted for Open Responses parity: %#v", key, body)
		}
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" || reasoning["summary"] != "concise" {
		t.Fatalf("reasoning mapping failed: %+v", reasoning)
	}

	streamBody, _, err := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, true)
	if err != nil {
		t.Fatalf("buildRequestBody(stream) error = %v", err)
	}
	if streamBody["stream"] != true {
		t.Fatalf("stream request should include stream=true, got %#v", streamBody["stream"])
	}
}

func TestBuildRequestBodyResponseFormatJSONParity(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "local-model")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	format := body["text"].(map[string]interface{})["format"].(map[string]interface{})
	if format["type"] != "json_object" {
		t.Fatalf("format = %#v, want json_object", format)
	}
	if _, ok := format["schema"]; ok {
		t.Fatalf("json_object format should not include schema: %#v", format)
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
	}
	body, _, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json",
			Schema:      schema,
			Name:        "answer_schema",
			Description: "Answer schema",
		},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"strictJsonSchema": false,
				"textVerbosity":    "high",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	text := body["text"].(map[string]interface{})
	format = text["format"].(map[string]interface{})
	if format["type"] != "json_schema" || format["name"] != "answer_schema" || format["description"] != "Answer schema" || format["schema"] == nil || format["strict"] != false || text["verbosity"] != "high" {
		t.Fatalf("format = %#v, want TS json_schema shape", format)
	}

	body, _, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"textVerbosity": "low"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	text = body["text"].(map[string]interface{})
	if _, ok := text["format"]; ok {
		t.Fatalf("verbosity-only text config should not add response format: %#v", text)
	}
	if text["verbosity"] != "low" {
		t.Fatalf("verbosity = %#v, want low", text["verbosity"])
	}

	body, _, err = model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "text"},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if _, ok := body["text"]; ok {
		t.Fatalf("non-json response format should not serialize text config like TS: %#v", body["text"])
	}
}

func TestBuildRequestBodyProviderOptionsParity(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5")

	high := types.ReasoningHigh
	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &high,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"conversation":         nil,
				"maxToolCalls":         3,
				"metadata":             map[string]interface{}{"trace": "abc"},
				"parallelToolCalls":    false,
				"previousResponseId":   "resp_123",
				"store":                false,
				"user":                 "user-1",
				"instructions":         "follow this",
				"serviceTier":          "priority",
				"include":              []string{"reasoning.encrypted_content"},
				"promptCacheKey":       "cache-key",
				"promptCacheRetention": "24h",
				"safetyIdentifier":     "safe-1",
				"truncation":           "disabled",
				"logprobs":             true,
				"reasoningEffort":      "minimal",
				"contextManagement": []map[string]interface{}{
					map[string]interface{}{"type": "compaction", "compactThreshold": 50000},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	if _, ok := body["conversation"]; !ok || body["conversation"] != nil {
		t.Fatalf("conversation should preserve explicit null: %#v", body["conversation"])
	}
	assertBodyValue(t, body, "max_tool_calls", 3)
	assertBodyValue(t, body, "metadata", map[string]interface{}{"trace": "abc"})
	assertBodyValue(t, body, "parallel_tool_calls", false)
	assertBodyValue(t, body, "previous_response_id", "resp_123")
	assertBodyValue(t, body, "store", false)
	assertBodyValue(t, body, "user", "user-1")
	assertBodyValue(t, body, "instructions", "follow this")
	assertBodyValue(t, body, "service_tier", "priority")
	assertBodyValue(t, body, "prompt_cache_key", "cache-key")
	assertBodyValue(t, body, "prompt_cache_retention", "24h")
	assertBodyValue(t, body, "safety_identifier", "safe-1")
	assertBodyValue(t, body, "truncation", "disabled")
	assertBodyValue(t, body, "top_logprobs", 20)
	include := body["include"].([]interface{})
	wantInclude := map[interface{}]bool{
		"reasoning.encrypted_content":  false,
		"message.output_text.logprobs": false,
	}
	if len(include) != len(wantInclude) {
		t.Fatalf("include = %#v, want TS store=false and logprobs includes", include)
	}
	for _, value := range include {
		if _, ok := wantInclude[value]; ok {
			wantInclude[value] = true
		}
	}
	for value, found := range wantInclude {
		if !found {
			t.Fatalf("include = %#v, missing %v", include, value)
		}
	}
	contextManagement := body["context_management"].([]map[string]interface{})
	if len(contextManagement) != 1 || contextManagement[0]["type"] != "compaction" || contextManagement[0]["compact_threshold"] != 50000 {
		t.Fatalf("context_management = %#v, want TS snake_case shape", contextManagement)
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "minimal" {
		t.Fatalf("reasoning = %#v, want provider reasoningEffort override", reasoning)
	}
}

func TestBuildRequestBodyConversationPreviousResponseWarning(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5")

	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"conversation":       "conv_123",
				"previousResponseId": "resp_123",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if body["conversation"] != "conv_123" || body["previous_response_id"] != "resp_123" {
		t.Fatalf("conversation fields should be preserved while warning like TS: %#v", body)
	}
	found := false
	for _, warning := range warnings {
		if warning.Type == "unsupported" &&
			warning.Feature == "conversation" &&
			warning.Details == "conversation and previousResponseId cannot be used together" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing conversation/previousResponseId warning: %+v", warnings)
	}
}

// TestBuildRequestBodyReasoningOptionsSentRegardlessOfModel covers rows
// 3b9f025/e69a836: Open Responses (unlike the OpenAI chat/Responses model
// packages) applies NO "is this a reasoning model" gating — reasoning
// options are always sent when resolved, and never produce an "unsupported"
// warning, even on a model OpenAI's own capability detection would classify
// as non-reasoning (e.g. gpt-4o). See TS open-responses-language-model.ts,
// which has no isReasoningModel check at all.
func TestBuildRequestBodyReasoningOptionsSentRegardlessOfModel(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-4o")

	high := types.ReasoningHigh
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &high,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"reasoningEffort":  "high",
				"reasoningSummary": "concise",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	reasoning, ok := body["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "high" || reasoning["summary"] != "concise" {
		t.Fatalf("reasoning = %#v, want effort=high summary=concise sent unconditionally", body["reasoning"])
	}
	for _, warning := range warnings {
		if warning.Feature == "reasoningEffort" || warning.Feature == "reasoningSummary" {
			t.Fatalf("reasoning options should never warn as unsupported on Open Responses: %+v", warnings)
		}
	}
}

// TestBuildRequestBodySamplingAlwaysSentAlongsideReasoning covers the same
// rows for sampling parameters: Open Responses never omits temperature/topP
// when reasoning is also set (that omission logic is OpenAI-package-specific
// and does not exist in open-responses-language-model.ts).
func TestBuildRequestBodySamplingAlwaysSentAlongsideReasoning(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5")

	temperature := 0.7
	topP := 0.9
	high := types.ReasoningHigh
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temperature,
		TopP:        &topP,
		Reasoning:   &high,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if body["temperature"] != temperature || body["top_p"] != topP {
		t.Fatalf("temperature/top_p should be sent unconditionally alongside reasoning: %#v", body)
	}
	for _, warning := range warnings {
		if warning.Feature == "temperature" || warning.Feature == "topP" {
			t.Fatalf("sampling should never warn as unsupported on Open Responses: %+v", warnings)
		}
	}
}

func TestBuildRequestBodyReasoningNoneKeepsSamplingOnSupportedModel(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5.1")

	temperature := 0.7
	topP := 0.9
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temperature,
		TopP:        &topP,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "none"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if body["temperature"] != temperature || body["top_p"] != topP {
		t.Fatalf("sampling should be preserved for gpt-5.1 reasoningEffort none: %#v", body)
	}
	for _, warning := range warnings {
		if warning.Feature == "temperature" || warning.Feature == "topP" {
			t.Fatalf("sampling should not warn for gpt-5.1 reasoningEffort none: %+v", warnings)
		}
	}
}

func TestBuildRequestBodyReasoningNoneKeepsSamplingOnLaterGPT5Families(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5.2")

	temperature := 0.7
	topP := 0.9
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temperature,
		TopP:        &topP,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "none"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if body["temperature"] != temperature || body["top_p"] != topP {
		t.Fatalf("sampling should be preserved for gpt-5.2 reasoningEffort none: %#v", body)
	}
	for _, warning := range warnings {
		if warning.Feature == "temperature" || warning.Feature == "topP" {
			t.Fatalf("sampling should not warn for gpt-5.2 reasoningEffort none: %+v", warnings)
		}
	}
}

// TestBuildRequestBodyGPT5ChatSendsReasoningButRejectsFlexTier verifies that
// gpt-5-chat-latest still gets its serviceTier "flex" rejected (a
// Go-specific model-capability check unrelated to rows 3b9f025/e69a836,
// mirroring OpenAI's own serviceTier support matrix), while reasoning
// options ARE sent unconditionally like any other model on Open Responses.
func TestBuildRequestBodyGPT5ChatSendsReasoningButRejectsFlexTier(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "gpt-5-chat-latest")

	temperature := 0.7
	topP := 0.9
	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temperature,
		TopP:        &topP,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"serviceTier":     "flex",
				"reasoningEffort": "high",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if body["temperature"] != temperature || body["top_p"] != topP {
		t.Fatalf("gpt-5-chat-latest should preserve sampling: %#v", body)
	}
	reasoning, ok := body["reasoning"].(map[string]interface{})
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning should be sent unconditionally on Open Responses: %#v", body["reasoning"])
	}
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("gpt-5-chat-latest should reject flex tier like TS: %#v", body)
	}
	for _, warning := range warnings {
		if warning.Feature == "reasoningEffort" {
			t.Fatalf("reasoningEffort should never warn as unsupported on Open Responses: %+v", warnings)
		}
	}
	assertUnsupportedWarning(t, warnings, "serviceTier")
}

func TestBuildRequestBodyServiceTierValidationParity(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})

	body, warnings, err := NewLanguageModel(p, "gpt-4o").buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"serviceTier": "flex"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(flex unsupported) error = %v", err)
	}
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("unsupported flex service tier should be omitted: %#v", body)
	}
	assertUnsupportedWarning(t, warnings, "serviceTier")

	body, warnings, err = NewLanguageModel(p, "gpt-5").buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"serviceTier": "flex"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(flex supported) error = %v", err)
	}
	if body["service_tier"] != "flex" {
		t.Fatalf("supported flex service tier should be preserved: %#v", body)
	}
	for _, warning := range warnings {
		if warning.Feature == "serviceTier" {
			t.Fatalf("supported flex service tier should not warn: %+v", warnings)
		}
	}

	body, warnings, err = NewLanguageModel(p, "gpt-5-nano").buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"serviceTier": "priority"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(priority unsupported) error = %v", err)
	}
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("unsupported priority service tier should be omitted: %#v", body)
	}
	assertUnsupportedWarning(t, warnings, "serviceTier")

	body, warnings, err = NewLanguageModel(p, "gpt-5.4-nano").buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"serviceTier": "priority"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(priority gpt-5.4-nano unsupported) error = %v", err)
	}
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("gpt-5.4-nano priority service tier should be omitted: %#v", body)
	}
	assertUnsupportedWarning(t, warnings, "serviceTier")
}

func TestBuildRequestBodyAllowedToolsParity(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "local-model")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "weather", Description: "weather", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "cityAttractions", Description: "attractions", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedTools": map[string]interface{}{
					"toolNames": []string{"weather"},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	if len(body["tools"].([]interface{})) != 2 {
		t.Fatalf("tools = %#v, want full tool list preserved", body["tools"])
	}
	choice := body["tool_choice"].(map[string]interface{})
	allowed := choice["tools"].([]map[string]interface{})
	if choice["type"] != "allowed_tools" || choice["mode"] != "auto" || len(allowed) != 1 || allowed[0]["type"] != "function" || allowed[0]["name"] != "weather" {
		t.Fatalf("tool_choice = %#v, want TS allowed_tools override", choice)
	}
}

func assertUnsupportedWarning(t *testing.T, warnings []types.Warning, feature string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Type == "unsupported" && warning.Feature == feature {
			return
		}
	}
	t.Fatalf("missing unsupported warning for %s: %+v", feature, warnings)
}

func assertBodyValue(t *testing.T, body map[string]interface{}, key string, want interface{}) {
	t.Helper()
	got, ok := body[key]
	if !ok {
		t.Fatalf("body missing %s in %#v", key, body)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s = %s, want %s", key, gotJSON, wantJSON)
	}
}

func TestOpenResponsesStreamHandleEvents(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)
	s.toolCallsByItemID["item-1"] = &toolCallState{ID: "call-1", ToolName: "weather"}

	_, _ = s.handleStreamEvent(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "item-1", Delta: `{"city":"`})
	_, _ = s.handleStreamEvent(&StreamEvent{Type: "response.function_call_arguments.done", ItemID: "item-1", Arguments: `{"city":"nyc"}`})

	chunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "function_call", ID: "item-1"},
	})
	if err != nil {
		t.Fatalf("function_call done error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolCall || chunk.ToolCall == nil || chunk.ToolCall.ToolName != "weather" {
		t.Fatalf("unexpected tool call chunk: %+v", chunk)
	}

	// A completed custom_tool_call enqueues tool-input-end then tool-call
	// (mirrors TS: two separate stream parts), so the tool-call itself is
	// read back via a follow-up Next() call from the pending queue.
	customEndChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "custom_tool_call", CallID: "call-2", Name: "custom", Input: "raw"},
	})
	if err != nil || customEndChunk.Type != provider.ChunkTypeToolInputEnd || customEndChunk.ID != "call-2" {
		t.Fatalf("custom_tool_call tool-input-end failed: chunk=%+v err=%v", customEndChunk, err)
	}
	customChunk, err := s.Next()
	if err != nil || customChunk.ToolCall == nil || customChunk.ToolCall.Arguments["input"] != "raw" {
		t.Fatalf("custom_tool_call conversion failed: chunk=%+v err=%v", customChunk, err)
	}
	if customChunk.ToolCall.RawArguments != `"raw"` {
		t.Fatalf("custom_tool_call RawArguments = %q, want %q", customChunk.ToolCall.RawArguments, `"raw"`)
	}

	reasoningChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "reasoning", ID: "r1", EncryptedContent: "enc"},
	})
	if err != nil || reasoningChunk.Type != provider.ChunkTypeReasoningEnd {
		t.Fatalf("reasoning chunk failed: chunk=%+v err=%v", reasoningChunk, err)
	}
	if !json.Valid(reasoningChunk.ProviderMetadata) {
		t.Fatalf("expected json provider metadata, got: %s", string(reasoningChunk.ProviderMetadata))
	}
	var reasoningMetadata map[string]map[string]interface{}
	if err := json.Unmarshal(reasoningChunk.ProviderMetadata, &reasoningMetadata); err != nil {
		t.Fatalf("provider metadata unmarshal: %v", err)
	}
	if _, ok := reasoningMetadata["openresponses"]; ok {
		t.Fatalf("legacy openresponses metadata key should not be emitted: %+v", reasoningMetadata)
	}
	currentMetadata, ok := reasoningMetadata["open-responses"]
	if !ok {
		t.Fatalf("provider metadata = %+v, want open-responses key", reasoningMetadata)
	}
	// Row a0d2e8c/6fe187f: full createReasoningProviderMetadata shape
	// ({itemId, reasoningSummary, reasoningContent, reasoningEncryptedContent}),
	// not just a bare "encryptedContent" key.
	if currentMetadata["reasoningEncryptedContent"] != "enc" || currentMetadata["itemId"] != "r1" {
		t.Fatalf("provider metadata payload = %+v", currentMetadata)
	}

	finishChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.completed",
		Response: &OpenResponsesResponse{
			IncompleteDetails: &IncompleteDetails{Reason: "max_output_tokens"},
			Usage:             &Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
		},
	})
	if err != nil || finishChunk.Type != provider.ChunkTypeFinish || finishChunk.Usage == nil {
		t.Fatalf("finish chunk failed: chunk=%+v err=%v", finishChunk, err)
	}

	// A bare "error" event is surfaced as an in-band ChunkTypeError chunk
	// (mirrors TS's controller.enqueue({type:'error',...})), not returned as
	// a fatal Next()/handleStreamEvent error -- the stream still ends
	// normally afterward.
	errChunk, err := s.handleStreamEvent(&StreamEvent{
		Type:  "error",
		Error: &ResponseError{Code: "bad_request", Message: "boom"},
	})
	if err != nil {
		t.Fatalf("unexpected error from stream error event: %v", err)
	}
	if errChunk.Type != provider.ChunkTypeError || errChunk.Text != "boom" {
		t.Fatalf("unexpected error chunk: %+v", errChunk)
	}
	if !providererrors.IsStreamProviderError(errChunk.Err) {
		t.Fatalf("expected a StreamProviderError, got %T: %v", errChunk.Err, errChunk.Err)
	}
}

// TestOpenResponsesStreamTextStartDeltaEnd verifies that a "message" output
// item streams text-start (on output_item.added), a text delta carrying the
// item id (on response.output_text.delta), and text-end with {itemId,
// annotations?} provider metadata (on output_item.done), mirroring TS's
// `{type:'text-start', id: chunk.item.id}` / `{type:'text-delta', id:
// chunk.item_id, delta: chunk.delta}` / `{type:'text-end', id:
// chunk.item.id, providerMetadata: {...}}`.
func TestOpenResponsesStreamTextStartDeltaEnd(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)

	startChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.added",
		Item: &OutputItem{Type: "message", ID: "msg_1"},
	})
	if err != nil || startChunk.Type != provider.ChunkTypeTextStart || startChunk.ID != "msg_1" {
		t.Fatalf("text-start failed: chunk=%+v err=%v", startChunk, err)
	}

	deltaChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_text.delta", ItemID: "msg_1", Delta: "hello",
	})
	if err != nil || deltaChunk.Type != provider.ChunkTypeText || deltaChunk.ID != "msg_1" || deltaChunk.Text != "hello" {
		t.Fatalf("text delta failed: chunk=%+v err=%v", deltaChunk, err)
	}

	endChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{
			Type: "message",
			ID:   "msg_1",
			Content: []ContentPart{
				{Type: "output_text", Text: "hello", Annotations: []Annotation{
					{Type: "url_citation", URL: "https://example.com", Title: "Example", StartIndex: 0, EndIndex: 5},
				}},
			},
		},
	})
	if err != nil || endChunk.Type != provider.ChunkTypeTextEnd || endChunk.ID != "msg_1" {
		t.Fatalf("text-end failed: chunk=%+v err=%v", endChunk, err)
	}
	var payload map[string]map[string]interface{}
	if unmarshalErr := json.Unmarshal(endChunk.ProviderMetadata, &payload); unmarshalErr != nil {
		t.Fatalf("ProviderMetadata unmarshal failed: %v", unmarshalErr)
	}
	meta := payload["open-responses"]
	if meta["itemId"] != "msg_1" {
		t.Fatalf("itemId = %v, want msg_1", meta["itemId"])
	}
	if annotations, ok := meta["annotations"].([]interface{}); !ok || len(annotations) != 1 {
		t.Fatalf("annotations = %+v, want 1 entry", meta["annotations"])
	}
}

func TestOpenResponsesStreamFunctionCallPreservesProviderMetadata(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)

	_, _ = s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.added",
		Item: &OutputItem{
			Type:      "function_call",
			ID:        "fc_item_1",
			CallID:    "call-1",
			Name:      "weather",
			Namespace: "weather",
		},
	})
	_, _ = s.handleStreamEvent(&StreamEvent{
		Type:      "response.function_call_arguments.done",
		ItemID:    "fc_item_1",
		Arguments: `{"city":"nyc"}`,
	})
	chunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "function_call", ID: "fc_item_1"},
	})
	if err != nil {
		t.Fatalf("function_call done error = %v", err)
	}
	if chunk.ToolCall == nil {
		t.Fatalf("expected tool call chunk, got %+v", chunk)
	}
	metadata, ok := chunk.ToolCall.ProviderMetadata["open-responses"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata = %+v, want open-responses payload", chunk.ToolCall.ProviderMetadata)
	}
	if metadata["itemId"] != "fc_item_1" || metadata["namespace"] != "weather" {
		t.Fatalf("provider metadata payload = %+v", metadata)
	}
}

// TestOpenResponsesStreamReasoningDeltaLifecycle covers row a0d2e8c:
// reasoning-start on output_item.added, reasoning deltas from both
// response.reasoning_summary_text.delta and response.reasoning_text.delta,
// and reasoning-end (with full metadata) on output_item.done.
func TestOpenResponsesStreamReasoningDeltaLifecycle(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)

	start, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.added",
		Item: &OutputItem{Type: "reasoning", ID: "r1"},
	})
	if err != nil || start.Type != provider.ChunkTypeReasoningStart || start.ID != "r1" {
		t.Fatalf("reasoning-start chunk = %+v, err=%v", start, err)
	}
	if s.activeReasoningID != "r1" {
		t.Fatalf("activeReasoningID = %q, want r1", s.activeReasoningID)
	}

	delta1, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.reasoning_summary_text.delta", ItemID: "r1", Delta: "thinking ",
	})
	if err != nil || delta1.Type != provider.ChunkTypeReasoning || delta1.ID != "r1" || delta1.Reasoning != "thinking " {
		t.Fatalf("reasoning delta (summary) = %+v, err=%v", delta1, err)
	}

	delta2, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.reasoning_text.delta", ItemID: "r1", Delta: "harder",
	})
	if err != nil || delta2.Type != provider.ChunkTypeReasoning || delta2.ID != "r1" || delta2.Reasoning != "harder" {
		t.Fatalf("reasoning delta (text, LM Studio extension) = %+v, err=%v", delta2, err)
	}

	end, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "reasoning", ID: "r1", Summary: []ContentPart{{Type: "summary_text", Text: "thinking harder"}}},
	})
	if err != nil || end.Type != provider.ChunkTypeReasoningEnd || end.ID != "r1" {
		t.Fatalf("reasoning-end chunk = %+v, err=%v", end, err)
	}
	if s.activeReasoningID != "" {
		t.Fatalf("activeReasoningID should be cleared after reasoning-end, got %q", s.activeReasoningID)
	}
}

// TestOpenResponsesStreamFlushClosesUnfinishedReasoning covers row 6fe187f:
// if the stream finishes while a reasoning block is still open (no matching
// output_item.done), a reasoning-end using the original item id is emitted
// before the finish chunk.
func TestOpenResponsesStreamFlushClosesUnfinishedReasoning(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)

	_, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.added",
		Item: &OutputItem{Type: "reasoning", ID: "r2"},
	})
	if err != nil {
		t.Fatalf("output_item.added error = %v", err)
	}

	first, err := s.handleStreamEvent(&StreamEvent{Type: "response.completed", Response: &OpenResponsesResponse{}})
	if err != nil {
		t.Fatalf("response.completed error = %v", err)
	}
	if first.Type != provider.ChunkTypeReasoningEnd || first.ID != "r2" {
		t.Fatalf("first chunk after unfinished reasoning = %+v, want reasoning-end for r2", first)
	}

	second, err := s.Next()
	if err != nil {
		t.Fatalf("Next() after flushed reasoning-end error = %v", err)
	}
	if second.Type != provider.ChunkTypeFinish {
		t.Fatalf("second chunk = %+v, want finish", second)
	}
}

// TestOpenResponsesStreamToolCallOutOfOrder covers row fb82a6c: deltas or a
// done event arriving without a prior output_item.added must not be dropped.
func TestOpenResponsesStreamToolCallOutOfOrder(t *testing.T) {
	t.Run("delta before add", func(t *testing.T) {
		s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)
		// The delta-only event accumulates and then calls s.Next() to read
		// the next SSE event; against this empty test reader that hits EOF,
		// which is expected here (a real stream would keep reading). What
		// matters is that the accumulator was created lazily despite no
		// prior output_item.added.
		_, _ = s.handleStreamEvent(&StreamEvent{
			Type: "response.function_call_arguments.delta", ItemID: "oo1", Delta: `{"q":"go"}`,
		})
		if _, ok := s.toolCallsByItemID["oo1"]; !ok {
			t.Fatalf("expected a lazily-created accumulator for oo1")
		}
		chunk, err := s.handleStreamEvent(&StreamEvent{
			Type: "response.output_item.done",
			Item: &OutputItem{Type: "function_call", ID: "oo1", CallID: "call-oo1", Name: "search"},
		})
		if err != nil {
			t.Fatalf("output_item.done error = %v", err)
		}
		if chunk.ToolCall == nil || chunk.ToolCall.Arguments["q"] != "go" {
			t.Fatalf("tool call = %+v, want arguments from the out-of-order delta", chunk.ToolCall)
		}
	})

	t.Run("done with no prior state", func(t *testing.T) {
		s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader("")}, nil)
		chunk, err := s.handleStreamEvent(&StreamEvent{
			Type: "response.output_item.done",
			Item: &OutputItem{Type: "function_call", ID: "oo2", CallID: "call-oo2", Name: "lookup", Arguments: `{"x":1}`},
		})
		if err != nil {
			t.Fatalf("output_item.done with no prior state error = %v", err)
		}
		if chunk.ToolCall == nil || chunk.ToolCall.ID != "call-oo2" || chunk.ToolCall.ToolName != "lookup" {
			t.Fatalf("tool call = %+v, want fallback from the done item's own fields", chunk.ToolCall)
		}
		if chunk.ToolCall.Arguments["x"] != float64(1) {
			t.Fatalf("tool call arguments = %+v, want x=1 from done item's raw arguments", chunk.ToolCall.Arguments)
		}
	})
}
