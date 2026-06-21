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
	tools := convertToolsToOpenResponses([]types.Tool{
		{Name: "weather", Description: "lookup", Parameters: map[string]interface{}{"type": "object"}, Strict: true},
	})
	if len(tools) != 1 || tools[0].Name != "weather" || !tools[0].Strict {
		t.Fatalf("tools conversion failed: %+v", tools)
	}

	if got := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "auto"}); got != "auto" {
		t.Fatalf("auto choice = %#v", got)
	}
	if got := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "required"}); got != "required" {
		t.Fatalf("required choice = %#v", got)
	}
	if got := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "none"}); got != "none" {
		t.Fatalf("none choice = %#v", got)
	}
	toolChoice := convertToolChoiceToOpenResponses(types.ToolChoice{Type: "tool", ToolName: "weather"})
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
	if tools, ok := body["tools"].([]FunctionTool); !ok || len(tools) != 0 {
		t.Fatalf("request should include empty tools array like TS, got %#v", body["tools"])
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

func TestBuildRequestBodyReasoningOptionsOnNonReasoningModel(t *testing.T) {
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
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("non-reasoning model should omit reasoning like TS: %#v", body["reasoning"])
	}
	warningFeatures := map[string]bool{}
	for _, warning := range warnings {
		if warning.Type == "unsupported" {
			warningFeatures[warning.Feature] = true
		}
	}
	for _, feature := range []string{"reasoningEffort", "reasoningSummary"} {
		if !warningFeatures[feature] {
			t.Fatalf("missing unsupported warning for %s: %+v", feature, warnings)
		}
	}
}

func TestBuildRequestBodyForceReasoningForCustomModel(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "local-model")

	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"forceReasoning":   true,
				"reasoningEffort":  "high",
				"reasoningSummary": "concise",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	reasoning := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" || reasoning["summary"] != "concise" {
		t.Fatalf("reasoning = %#v, want forced reasoning payload", reasoning)
	}
	for _, warning := range warnings {
		if warning.Feature == "reasoningEffort" || warning.Feature == "reasoningSummary" {
			t.Fatalf("forced reasoning should not warn for reasoning options: %+v", warnings)
		}
	}
}

func TestBuildRequestBodyReasoningModelOmitsUnsupportedSampling(t *testing.T) {
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
	for _, key := range []string{"temperature", "top_p"} {
		if _, ok := body[key]; ok {
			t.Fatalf("%s should be omitted for reasoning models like TS: %#v", key, body)
		}
	}
	warningFeatures := map[string]bool{}
	for _, warning := range warnings {
		if warning.Type == "unsupported" {
			warningFeatures[warning.Feature] = true
		}
	}
	for _, feature := range []string{"temperature", "topP"} {
		if !warningFeatures[feature] {
			t.Fatalf("missing unsupported warning for %s: %+v", feature, warnings)
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

func TestBuildRequestBodyGPT5ChatUsesNonReasoningCapabilities(t *testing.T) {
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
		t.Fatalf("gpt-5-chat-latest should preserve sampling as a non-reasoning model: %#v", body)
	}
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("gpt-5-chat-latest should not serialize reasoning without forceReasoning: %#v", body)
	}
	if _, ok := body["service_tier"]; ok {
		t.Fatalf("gpt-5-chat-latest should reject flex tier like TS: %#v", body)
	}
	assertUnsupportedWarning(t, warnings, "reasoningEffort")
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
	if len(body["tools"].([]FunctionTool)) != 2 {
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

	customChunk, err := s.handleStreamEvent(&StreamEvent{
		Type: "response.output_item.done",
		Item: &OutputItem{Type: "custom_tool_call", CallID: "call-2", Name: "custom", Input: "raw"},
	})
	if err != nil || customChunk.ToolCall == nil || customChunk.ToolCall.Arguments["input"] != "raw" {
		t.Fatalf("custom_tool_call conversion failed: chunk=%+v err=%v", customChunk, err)
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
	if currentMetadata["encryptedContent"] != "enc" || currentMetadata["itemId"] != "r1" {
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

	_, err = s.handleStreamEvent(&StreamEvent{
		Type:  "error",
		Error: &ResponseError{Code: "bad_request", Message: "boom"},
	})
	if err == nil {
		t.Fatal("expected stream error event to return error")
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
