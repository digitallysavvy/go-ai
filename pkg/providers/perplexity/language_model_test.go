package perplexity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// This file ports packages/perplexity/src/perplexity-language-model.test.ts
// (and, where relevant, convert-to-perplexity-input.test.ts /
// convert-perplexity-usage.test.ts) to Go table/behavior tests against the
// Agent API. TS test names are cited in comments.

var testPrompt = []types.Message{
	{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
}

func createTestUsage() map[string]interface{} {
	return map[string]interface{}{
		"input_tokens": 120,
		"input_tokens_details": map[string]interface{}{
			"cache_creation_input_tokens": 10,
			"cache_read_input_tokens":     20,
		},
		"output_tokens":         45,
		"output_tokens_details": map[string]interface{}{"reasoning_tokens": 5},
		"total_tokens":          165,
		"tool_calls_details":    map[string]interface{}{"search_web": map[string]interface{}{"invocation": 2}},
		"cost": map[string]interface{}{
			"currency":        "USD",
			"input_cost":      0.001,
			"output_cost":     0.002,
			"tool_calls_cost": 0.003,
			"total_cost":      0.006,
		},
	}
}

func createTestResponse(overrides map[string]interface{}) map[string]interface{} {
	base := map[string]interface{}{
		"id":         "resp-123",
		"created_at": 1784292159,
		"model":      "openai/gpt-5.1",
		"object":     "response",
		"output": []interface{}{
			map[string]interface{}{
				"type": "search_results",
				"results": []interface{}{
					map[string]interface{}{
						"id":      1,
						"title":   "Example source",
						"url":     "https://example.com/source",
						"snippet": "An example search result.",
						"date":    "2026-08-01",
						"source":  "web",
					},
				},
			},
			map[string]interface{}{
				"id":     "msg-123",
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type":        "output_text",
						"text":        "Hello from Perplexity.",
						"annotations": []interface{}{},
					},
				},
			},
		},
		"status": "completed",
		"usage":  createTestUsage(),
	}
	for k, v := range overrides {
		if v == nil {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	return base
}

func newPerplexityTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *LanguageModel) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prov := New(Config{BaseURL: srv.URL, APIKey: "test-token"})
	model := NewLanguageModel(prov, "low")
	return srv, model
}

func jsonResponseHandler(t *testing.T, body map[string]interface{}, extraHeaders map[string]string, capturedBody *map[string]interface{}) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if capturedBody != nil {
			var got map[string]interface{}
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &got)
			*capturedBody = got
		}
		for k, v := range extraHeaders {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}

// TS: "sends an Agent API preset request"
func TestPerplexityDoGenerate_PresetRequest(t *testing.T) {
	var captured map[string]interface{}
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, &captured))

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if captured["preset"] != "low" {
		t.Fatalf("preset = %v, want low", captured["preset"])
	}
	if _, ok := captured["model"]; ok {
		t.Fatalf("model key should be absent for a preset request, got %v", captured["model"])
	}
	input, ok := captured["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Fatalf("input = %v, want single user message", captured["input"])
	}
}

// TS: "sends direct model IDs as Agent API models"
func TestPerplexityDoGenerate_DirectModelID(t *testing.T) {
	var captured map[string]interface{}
	srv, _ := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, &captured))
	prov := New(Config{BaseURL: srv.URL})
	model := NewLanguageModel(prov, "openai/gpt-5.1")

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if captured["model"] != "openai/gpt-5.1" {
		t.Fatalf("model = %v, want openai/gpt-5.1", captured["model"])
	}
	if _, ok := captured["preset"]; ok {
		t.Fatal("preset key should be absent for a direct model ID request")
	}
}

// TS: "does not map legacy Sonar model IDs to Agent API presets"
func TestPerplexityDoGenerate_LegacySonarModelIsNotAPreset(t *testing.T) {
	var captured map[string]interface{}
	srv, _ := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, &captured))
	prov := New(Config{BaseURL: srv.URL})
	model := NewLanguageModel(prov, "sonar-pro")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if captured["model"] != "sonar-pro" {
		t.Fatalf("model = %v, want sonar-pro", captured["model"])
	}
	for _, w := range result.Warnings {
		if w.Type == "deprecated" {
			t.Fatalf("unexpected deprecated warning: %+v", w)
		}
	}
}

// TS: "extracts text, sources, usage, cost, and response metadata"
func TestPerplexityDoGenerate_ExtractsContentUsageAndMetadata(t *testing.T) {
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, nil))

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content len = %d, want 2 (source + text)", len(result.Content))
	}
	source, ok := result.Content[0].(types.SourceContent)
	if !ok || source.ID != "1" || source.URL != "https://example.com/source" || source.Title != "Example source" {
		t.Fatalf("content[0] = %#v, want source id=1", result.Content[0])
	}
	text, ok := result.Content[1].(types.TextContent)
	if !ok || text.Text != "Hello from Perplexity." {
		t.Fatalf("content[1] = %#v, want text", result.Content[1])
	}

	u := result.Usage
	if u.InputTokens == nil || *u.InputTokens != 120 {
		t.Fatalf("InputTokens = %v, want 120", u.InputTokens)
	}
	if u.InputDetails == nil || *u.InputDetails.NoCacheTokens != 90 || *u.InputDetails.CacheReadTokens != 20 || *u.InputDetails.CacheWriteTokens != 10 {
		t.Fatalf("InputDetails = %+v, want noCache=90 cacheRead=20 cacheWrite=10", u.InputDetails)
	}
	if u.OutputTokens == nil || *u.OutputTokens != 45 {
		t.Fatalf("OutputTokens = %v, want 45", u.OutputTokens)
	}
	if u.OutputDetails == nil || *u.OutputDetails.TextTokens != 40 || *u.OutputDetails.ReasoningTokens != 5 {
		t.Fatalf("OutputDetails = %+v, want text=40 reasoning=5", u.OutputDetails)
	}

	meta, ok := result.ProviderMetadata["perplexity"].(PerplexityMetadata)
	if !ok {
		t.Fatalf("ProviderMetadata['perplexity'] type = %T", result.ProviderMetadata["perplexity"])
	}
	if meta.Usage.NumSearchQueries == nil || *meta.Usage.NumSearchQueries != 2 {
		t.Fatalf("NumSearchQueries = %v, want 2", meta.Usage.NumSearchQueries)
	}
	if meta.Cost == nil || meta.Cost.TotalCost == nil || *meta.Cost.TotalCost != 0.006 || meta.Cost.ToolCallsCost == nil || *meta.Cost.ToolCallsCost != 0.003 {
		t.Fatalf("Cost = %+v, want totalCost=0.006 toolCallsCost=0.003", meta.Cost)
	}
	if meta.ToolCalls["search_web"].Invocation == nil || *meta.ToolCalls["search_web"].Invocation != 2 {
		t.Fatalf("ToolCalls = %+v, want search_web invocation=2", meta.ToolCalls)
	}
	if meta.Images != nil {
		t.Fatalf("Images = %v, want nil (always null)", meta.Images)
	}

	if result.ResponseMetadata == nil || result.ResponseMetadata.ID != "resp-123" || result.ResponseMetadata.ModelID != "openai/gpt-5.1" {
		t.Fatalf("ResponseMetadata = %+v", result.ResponseMetadata)
	}
	if result.ResponseMetadata.Timestamp.Unix() != 1784292159 {
		t.Fatalf("Timestamp = %v, want 1784292159", result.ResponseMetadata.Timestamp.Unix())
	}
}

// TS: "accepts native tool results without treating them as web search results"
func TestPerplexityDoGenerate_IgnoresUnhandledNativeToolOutput(t *testing.T) {
	financeOutput := map[string]interface{}{
		"type":       "finance_results",
		"categories": []interface{}{"quote"},
		"tickers":    []interface{}{"AAPL"},
		"results": []interface{}{
			map[string]interface{}{"category": "quote", "content": "AAPL: $230.00", "sources": []interface{}{"https://example.com/quote"}, "tickers": []interface{}{"AAPL"}},
		},
	}
	base := createTestResponse(nil)
	output := append([]interface{}{financeOutput}, base["output"].([]interface{})...)
	response := createTestResponse(map[string]interface{}{"output": output})
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	found := false
	for _, part := range result.Content {
		if text, ok := part.(types.TextContent); ok && text.Text == "Hello from Perplexity." {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected text content, got %+v", result.Content)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %v, want stop", result.FinishReason)
	}
	var bodyGeneric map[string]interface{}
	b, _ := json.Marshal(response)
	_ = json.Unmarshal(b, &bodyGeneric)
	if result.ResponseMetadata == nil {
		t.Fatal("ResponseMetadata is nil")
	}
}

// TS: "still rejects malformed handled output items"
func TestPerplexityDoGenerate_RejectsMalformedSearchResult(t *testing.T) {
	response := createTestResponse(map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{"type": "search_results", "results": []interface{}{map[string]interface{}{"title": "Missing URL"}}},
		},
	})
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err == nil {
		t.Fatal("expected error for malformed search result missing url")
	}
}

// TS: "rejects malformed successful responses"
func TestPerplexityDoGenerate_RejectsResponseMissingOutput(t *testing.T) {
	response := map[string]interface{}{
		"id": "resp-123", "created_at": 1784292159, "model": "openai/gpt-5.1", "object": "response", "status": "completed",
	}
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err == nil {
		t.Fatal("expected error for response missing required 'output' field")
	}
}

// TS: "passes Agent API provider options and AI SDK function tools"
func TestPerplexityDoGenerate_ProviderOptionsAndFunctionTools(t *testing.T) {
	var captured map[string]interface{}
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, &captured))

	maxTokens := 200
	temperature := 0.4
	topP := 0.9
	reasoning := types.ReasoningHigh
	strict := true

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt:      types.Prompt{Messages: testPrompt},
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		TopP:        &topP,
		Reasoning:   &reasoning,
		ResponseFormat: &provider.ResponseFormat{
			Type: "json",
			Name: "answer",
			Schema: map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
				"required":             []interface{}{"answer"},
				"additionalProperties": false,
			},
		},
		Tools: []types.Tool{
			{
				Name:        "weather",
				Description: "Get the weather",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
					"required":   []interface{}{"city"},
				},
				Strict: &strict,
			},
		},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{
				"max_steps":            float64(4),
				"previous_response_id": "resp-previous",
				"store":                false,
				"tools":                []interface{}{map[string]interface{}{"type": "web_search", "search_context_size": "low"}},
				"future_option":        map[string]interface{}{"enabled": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if captured["preset"] != "low" || captured["max_output_tokens"] != float64(200) || captured["temperature"] != 0.4 || captured["top_p"] != 0.9 {
		t.Fatalf("standardized params missing: %+v", captured)
	}
	reasoningMap, ok := captured["reasoning"].(map[string]interface{})
	if !ok || reasoningMap["effort"] != "high" {
		t.Fatalf("reasoning = %v, want {effort: high}", captured["reasoning"])
	}
	if captured["max_steps"] != float64(4) || captured["previous_response_id"] != "resp-previous" || captured["store"] != false {
		t.Fatalf("agent options missing: %+v", captured)
	}
	future, ok := captured["future_option"].(map[string]interface{})
	if !ok || future["enabled"] != true {
		t.Fatalf("future_option passthrough missing: %v", captured["future_option"])
	}
	respFormat, ok := captured["response_format"].(map[string]interface{})
	if !ok || respFormat["type"] != "json_schema" {
		t.Fatalf("response_format = %v", captured["response_format"])
	}
	jsonSchema := respFormat["json_schema"].(map[string]interface{})
	if jsonSchema["name"] != "answer" || jsonSchema["strict"] != true {
		t.Fatalf("json_schema = %v", jsonSchema)
	}
	if _, ok := jsonSchema["description"]; ok {
		t.Fatalf("json_schema should omit description when unset, got %v", jsonSchema["description"])
	}
	tools, ok := captured["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v, want [native, function]", captured["tools"])
	}
	nativeTool := tools[0].(map[string]interface{})
	if nativeTool["type"] != "web_search" {
		t.Fatalf("tools[0] = %v, want native web_search tool first", nativeTool)
	}
	functionTool := tools[1].(map[string]interface{})
	if functionTool["type"] != "function" || functionTool["name"] != "weather" || functionTool["strict"] != true {
		t.Fatalf("tools[1] = %v, want function tool weather", functionTool)
	}
}

// TS: "rejects invalid provider options"
func TestPerplexityDoGenerate_RejectsInvalidProviderOptions(t *testing.T) {
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, nil))

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Messages: testPrompt},
		ProviderOptions: map[string]interface{}{"perplexity": map[string]interface{}{"max_steps": float64(0)}},
	})
	if err == nil {
		t.Fatal("expected InvalidArgumentError for max_steps: 0")
	}
}

// Mirrors TS perplexity-language-model-options.ts's nativeToolSchema union:
// providerOptions.perplexity.tools entries are validated per-type
// (mcp/connector require specific fields; web_search enums are checked).
// No upstream TS test exercises this directly, but the zod schema does
// reject these shapes, so a malformed native tool must fail the same way a
// malformed top-level provider option does (InvalidArgumentError).
func TestPerplexityDoGenerate_RejectsMalformedNativeTool(t *testing.T) {
	cases := []struct {
		name string
		tool map[string]interface{}
	}{
		{"mcp missing server_label", map[string]interface{}{"type": "mcp", "server_url": "https://example.com/mcp"}},
		{"mcp missing server_url", map[string]interface{}{"type": "mcp", "server_label": "my-mcp"}},
		{"connector missing id", map[string]interface{}{"type": "connector", "server_label": "my-connector"}},
		{"connector missing server_label", map[string]interface{}{"type": "connector", "id": "conn-1"}},
		{"web_search invalid search_recency_filter", map[string]interface{}{"type": "web_search", "filters": map[string]interface{}{"search_recency_filter": "decade"}}},
		{"web_search invalid search_context_size", map[string]interface{}{"type": "web_search", "search_context_size": "extreme"}},
		{"tool missing type", map[string]interface{}{"server_label": "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, nil))
			_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
				Prompt: types.Prompt{Messages: testPrompt},
				ProviderOptions: map[string]interface{}{
					"perplexity": map[string]interface{}{"tools": []interface{}{tc.tool}},
				},
			})
			if err == nil {
				t.Fatalf("expected error for malformed native tool %+v", tc.tool)
			}
		})
	}
}

// A future/unrecognized native tool type is forwarded unchecked, matching
// the forward-compatible passthrough for the rest of providerOptions.perplexity.
func TestPerplexityDoGenerate_AllowsUnrecognizedNativeToolType(t *testing.T) {
	var captured map[string]interface{}
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, createTestResponse(nil), nil, &captured))
	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: testPrompt},
		ProviderOptions: map[string]interface{}{
			"perplexity": map[string]interface{}{"tools": []interface{}{map[string]interface{}{"type": "future_tool", "config": "x"}}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v, want unrecognized native tool type to pass through", err)
	}
	tools, ok := captured["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", captured["tools"])
	}
}

// TS: "extracts function calls" + "omits missing function call thought signatures"
func TestPerplexityDoGenerate_ExtractsFunctionCalls(t *testing.T) {
	response := createTestResponse(map[string]interface{}{
		"status": "requires_action",
		"output": []interface{}{
			map[string]interface{}{
				"id": "fc-123", "type": "function_call", "status": "completed",
				"call_id": "call-123", "name": "weather", "arguments": `{"city":"San Francisco"}`,
				"thought_signature": "signature-123",
			},
		},
	})
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v, want 1 tool-call", result.Content)
	}
	tc, ok := result.Content[0].(types.ToolCallContent)
	if !ok || tc.ToolCallID != "call-123" || tc.ToolName != "weather" || tc.Input != `{"city":"San Francisco"}` {
		t.Fatalf("content[0] = %#v", result.Content[0])
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(tc.ProviderMetadata, &meta); err != nil {
		t.Fatalf("ProviderMetadata unmarshal error = %v", err)
	}
	if meta["perplexity"]["itemId"] != "fc-123" || meta["perplexity"]["thoughtSignature"] != "signature-123" {
		t.Fatalf("ProviderMetadata = %v", meta)
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls", result.FinishReason)
	}

	// omits missing thought signature
	response2 := createTestResponse(map[string]interface{}{
		"status": "requires_action",
		"output": []interface{}{
			map[string]interface{}{"id": "fc-123", "type": "function_call", "call_id": "call-123", "name": "weather", "arguments": `{"city":"San Francisco"}`},
		},
	})
	_, model2 := newPerplexityTestServer(t, jsonResponseHandler(t, response2, nil, nil))
	result2, err := model2.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	tc2 := result2.Content[0].(types.ToolCallContent)
	var meta2 map[string]map[string]interface{}
	_ = json.Unmarshal(tc2.ProviderMetadata, &meta2)
	if _, ok := meta2["perplexity"]["thoughtSignature"]; ok {
		t.Fatalf("thoughtSignature should be omitted, got %v", meta2["perplexity"])
	}
}

// TS: "maps incomplete reason %s to %s"
func TestPerplexityDoGenerate_MapsIncompleteReasons(t *testing.T) {
	cases := []struct {
		reason string
		want   types.FinishReason
	}{
		{"max_output_tokens", types.FinishReasonLength},
		{"content_filter", types.FinishReasonContentFilter},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			response := createTestResponse(map[string]interface{}{
				"status":             "incomplete",
				"incomplete_details": map[string]interface{}{"reason": tc.reason},
			})
			_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))
			result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
			if err != nil {
				t.Fatalf("DoGenerate() error = %v", err)
			}
			if result.FinishReason != tc.want {
				t.Fatalf("FinishReason = %v, want %v", result.FinishReason, tc.want)
			}
		})
	}
}

// TS: "throws an API call error for failed responses returned with HTTP 200"
func TestPerplexityDoGenerate_FailedWithHTTP200(t *testing.T) {
	response := createTestResponse(map[string]interface{}{
		"status": "failed",
		"error":  map[string]interface{}{"message": "Agent run failed", "type": "server_error"},
		"output": []interface{}{},
	})
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))

	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	if !containsMessage(err.Error(), "Agent run failed") {
		t.Fatalf("error = %v, want message containing 'Agent run failed'", err)
	}
}

func containsMessage(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TS: "keeps the search result ID when an annotation with the same URL appears first"
func TestPerplexityDoGenerate_SourceDedupKeepsResultID(t *testing.T) {
	response := createTestResponse(map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"id": "msg-123", "type": "message", "role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type": "output_text", "text": "Answer",
						"annotations": []interface{}{
							map[string]interface{}{"type": "url_citation", "url": "https://example.com/source", "title": "Annotation title"},
						},
					},
				},
			},
			map[string]interface{}{
				"type": "search_results",
				"results": []interface{}{
					map[string]interface{}{"id": 7, "title": "Search result title", "url": "https://example.com/source", "snippet": "Search result snippet."},
				},
			},
		},
	})
	_, model := newPerplexityTestServer(t, jsonResponseHandler(t, response, nil, nil))

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	var source *types.SourceContent
	for _, part := range result.Content {
		if s, ok := part.(types.SourceContent); ok {
			source = &s
		}
	}
	if source == nil {
		t.Fatal("expected a source content part")
	}
	if source.ID != "7" || source.Title != "Search result title" {
		t.Fatalf("source = %+v, want id=7 title=Search result title", source)
	}
}

// TS: "passes request and provider headers and exposes response headers"
func TestPerplexityDoGenerate_HeadersRoundTrip(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Test-Header", "test-value")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(createTestResponse(nil))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, Headers: map[string]string{"Custom-Provider-Header": "provider-value"}})
	model := NewLanguageModel(prov, "fast")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt:  types.Prompt{Messages: testPrompt},
		Headers: map[string]string{"Custom-Request-Header": "request-value"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotHeaders.Get("Custom-Provider-Header") != "provider-value" {
		t.Fatalf("provider header missing: %v", gotHeaders)
	}
	if gotHeaders.Get("Custom-Request-Header") != "request-value" {
		t.Fatalf("request header missing: %v", gotHeaders)
	}
	if result.ResponseHeaders["Test-Header"] != "test-value" {
		t.Fatalf("response headers = %v, want Test-Header", result.ResponseHeaders)
	}
}

// --- streaming tests ---

func streamHandler(t *testing.T, chunks []map[string]interface{}, capturedBody *map[string]interface{}) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if capturedBody != nil {
			var got map[string]interface{}
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &got)
			*capturedBody = got
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			b, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func createTestStreamChunks(responseOverrides map[string]interface{}) []map[string]interface{} {
	completed := createTestResponse(responseOverrides)
	return []map[string]interface{}{
		{"type": "response.created", "sequence_number": 0, "response": createTestResponse(map[string]interface{}{"output": []interface{}{}, "usage": nil})},
		{"type": "response.reasoning.search_results", "sequence_number": 1, "results": []interface{}{
			map[string]interface{}{"id": 1, "title": "Example source", "url": "https://example.com/source", "snippet": "An example search result.", "source": "web"},
		}},
		{"type": "response.output_text.delta", "sequence_number": 2, "item_id": "msg-123", "output_index": 1, "content_index": 0, "delta": "Hello "},
		{"type": "response.output_text.delta", "sequence_number": 3, "item_id": "msg-123", "output_index": 1, "content_index": 0, "delta": "from Perplexity."},
		{"type": "response.output_text.done", "sequence_number": 4, "item_id": "msg-123", "output_index": 1, "content_index": 0, "text": "Hello from Perplexity."},
		{"type": "response.completed", "sequence_number": 5, "response": completed},
	}
}

func collectChunks(t *testing.T, stream provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		chunks = append(chunks, c)
	}
	return chunks
}

// TS: "streams typed Agent API events as text, sources, usage, and metadata"
func TestPerplexityDoStream_TypedEvents(t *testing.T) {
	_, model := newPerplexityTestServer(t, streamHandler(t, createTestStreamChunks(nil), nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	chunks := collectChunks(t, stream)

	if chunks[0].Type != provider.ChunkTypeStreamStart {
		t.Fatalf("chunks[0] = %+v, want stream-start", chunks[0])
	}
	if chunks[1].Type != provider.ChunkTypeResponseMetadata || chunks[1].ResponseMetadata.ID != "resp-123" {
		t.Fatalf("chunks[1] = %+v, want response-metadata", chunks[1])
	}

	var sawSource, sawTextStart, sawTextEnd bool
	var text string
	var finish *provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeSource:
			sawSource = true
		case provider.ChunkTypeTextStart:
			sawTextStart = true
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeTextEnd:
			sawTextEnd = true
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if !sawSource || !sawTextStart || !sawTextEnd {
		t.Fatalf("missing expected chunk types: source=%v textStart=%v textEnd=%v", sawSource, sawTextStart, sawTextEnd)
	}
	if text != "Hello from Perplexity." {
		t.Fatalf("text = %q, want %q", text, "Hello from Perplexity.")
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.RawFinishReason != "completed" {
		t.Fatalf("finish = %+v, want stop/completed", finish)
	}
	if finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 120 {
		t.Fatalf("finish usage = %+v, want inputTokens=120", finish.Usage)
	}
	var meta map[string]PerplexityMetadata
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatalf("finish.ProviderMetadata unmarshal error = %v", err)
	}
	perp := meta["perplexity"]
	if perp.Cost == nil || perp.Cost.TotalCost == nil || *perp.Cost.TotalCost != 0.006 {
		t.Fatalf("finish provider metadata cost = %+v, want totalCost=0.006", perp.Cost)
	}
	if perp.ToolCalls["search_web"].Invocation == nil || *perp.ToolCalls["search_web"].Invocation != 2 {
		t.Fatalf("finish provider metadata toolCalls = %+v", perp.ToolCalls)
	}
}

// TS: "sends the Agent API streaming request body"
func TestPerplexityDoStream_RequestBody(t *testing.T) {
	var captured map[string]interface{}
	_, model := newPerplexityTestServer(t, streamHandler(t, createTestStreamChunks(nil), &captured))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	_ = collectChunks(t, stream)
	_ = stream.Close()

	if captured["preset"] != "low" || captured["stream"] != true {
		t.Fatalf("captured body = %+v, want preset=low stream=true", captured)
	}
}

// TS: "streams raw Agent API events when requested"
func TestPerplexityDoStream_IncludeRawChunks(t *testing.T) {
	chunks := createTestStreamChunks(nil)
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}, IncludeRawChunks: true})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var rawCount int
	for _, c := range got {
		if c.Type == provider.ChunkTypeRaw {
			rawCount++
		}
	}
	if rawCount != len(chunks) {
		t.Fatalf("raw chunk count = %d, want %d", rawCount, len(chunks))
	}
}

// TS: "streams function calls from output items"
func TestPerplexityDoStream_FunctionCalls(t *testing.T) {
	functionCall := map[string]interface{}{
		"id": "fc-123", "type": "function_call", "status": "completed",
		"call_id": "call-123", "name": "weather", "arguments": `{"city":"San Francisco"}`,
	}
	chunks := []map[string]interface{}{
		{"type": "response.created", "sequence_number": 0, "response": createTestResponse(map[string]interface{}{"output": []interface{}{}, "usage": nil})},
		{"type": "response.output_item.done", "sequence_number": 1, "output_index": 0, "item": functionCall},
		{"type": "response.completed", "sequence_number": 2, "response": createTestResponse(map[string]interface{}{"status": "requires_action", "output": []interface{}{functionCall}})},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var sawStart, sawDelta, sawEnd, sawCall bool
	var finish *provider.StreamChunk
	for _, c := range got {
		switch c.Type {
		case provider.ChunkTypeToolInputStart:
			if c.ToolCall != nil && c.ToolCall.ID == "call-123" && c.ToolCall.ToolName == "weather" {
				sawStart = true
			}
		case provider.ChunkTypeToolInputDelta:
			if c.ID == "call-123" && c.Text == `{"city":"San Francisco"}` {
				sawDelta = true
			}
		case provider.ChunkTypeToolInputEnd:
			if c.ToolCall != nil && c.ToolCall.ID == "call-123" {
				sawEnd = true
			}
		case provider.ChunkTypeToolCall:
			if c.ToolCall != nil && c.ToolCall.ID == "call-123" && c.ToolCall.ToolName == "weather" {
				sawCall = true
			}
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if !sawStart || !sawDelta || !sawEnd || !sawCall {
		t.Fatalf("missing tool call chunks: start=%v delta=%v end=%v call=%v", sawStart, sawDelta, sawEnd, sawCall)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonToolCalls || finish.RawFinishReason != "requires_action" {
		t.Fatalf("finish = %+v, want tool-calls/requires_action", finish)
	}
}

// TS: "accepts null fields in stream events" (6da8aa06d6)
func TestPerplexityDoStream_ToleratesNullFields(t *testing.T) {
	chunks := []map[string]interface{}{
		{"type": "response.reasoning.started", "sequence_number": 0, "thought": nil},
		{"type": "response.reasoning.fetch_url_results", "call_id": "call-1", "sequence_number": 1, "thought": "Fetched content from 0 URLs", "contents": nil},
		{"type": "response.reasoning.search_results", "sequence_number": 2, "results": nil},
		{"type": "response.reasoning.stopped", "sequence_number": 3},
		{"type": "response.output_text.delta", "item_id": "msg-1", "output_index": 0, "content_index": nil, "delta": "Hello"},
		{"type": "response.output_text.done", "item_id": "msg-1", "output_index": 0, "content_index": nil, "text": nil},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	for _, c := range got {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %+v", c)
		}
	}
	var reasoningDelta string
	var sawReasoningStart, sawReasoningEnd, sawTextStart, sawTextEnd bool
	var text string
	for _, c := range got {
		switch c.Type {
		case provider.ChunkTypeReasoningStart:
			sawReasoningStart = true
			if c.ID != "reasoning-0" {
				t.Fatalf("reasoning id = %q, want reasoning-0", c.ID)
			}
		case provider.ChunkTypeReasoning:
			reasoningDelta += c.Reasoning
		case provider.ChunkTypeReasoningEnd:
			sawReasoningEnd = true
		case provider.ChunkTypeTextStart:
			sawTextStart = true
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeTextEnd:
			sawTextEnd = true
		}
	}
	if !sawReasoningStart || !sawReasoningEnd {
		t.Fatalf("reasoning start/end missing: start=%v end=%v", sawReasoningStart, sawReasoningEnd)
	}
	if reasoningDelta != "Fetched content from 0 URLs" {
		t.Fatalf("reasoning delta = %q", reasoningDelta)
	}
	if !sawTextStart || !sawTextEnd || text != "Hello" {
		t.Fatalf("text handling wrong: start=%v end=%v text=%q", sawTextStart, sawTextEnd, text)
	}
}

// TS: "emits a fetched URL only once with its later search result ID"
func TestPerplexityDoStream_FetchedURLUpgradedByLaterSearchResult(t *testing.T) {
	chunks := []map[string]interface{}{
		{"type": "response.reasoning.fetch_url_results", "sequence_number": 0, "contents": []interface{}{
			map[string]interface{}{"title": "Fetched page", "url": "https://example.com/source", "snippet": "Fetched content."},
		}},
		{"type": "response.reasoning.search_results", "sequence_number": 1, "results": []interface{}{
			map[string]interface{}{"id": 7, "title": "Search result", "url": "https://example.com/source", "snippet": "Search result content."},
		}},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var sources []*provider.StreamChunk
	for _, c := range got {
		if c.Type == provider.ChunkTypeSource {
			sources = append(sources, c)
		}
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(sources))
	}
	if sources[0].SourceContent.ID != "7" {
		t.Fatalf("source id = %q, want 7", sources[0].SourceContent.ID)
	}
}

// TS: "handles incomplete terminal events and preserves usage"
func TestPerplexityDoStream_IncompleteTerminalEvent(t *testing.T) {
	chunks := []map[string]interface{}{
		{"type": "response.incomplete", "sequence_number": 0, "response": createTestResponse(map[string]interface{}{
			"status": "incomplete", "incomplete_details": map[string]interface{}{"reason": "max_output_tokens"},
		})},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var finish *provider.StreamChunk
	var text string
	for _, c := range got {
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if text != "Hello from Perplexity." {
		t.Fatalf("text = %q", text)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonLength || finish.RawFinishReason != "max_output_tokens" {
		t.Fatalf("finish = %+v", finish)
	}
	if finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 120 {
		t.Fatalf("usage not preserved: %+v", finish.Usage)
	}
}

// TS: "appends only missing text and does not repeat completed content parts"
func TestPerplexityDoStream_AppendsOnlyMissingText(t *testing.T) {
	message := map[string]interface{}{
		"type": "message", "id": "msg-123",
		"content": []interface{}{
			map[string]interface{}{"type": "output_text", "text": "Hello world."},
			map[string]interface{}{"type": "output_text", "text": "Second part."},
		},
	}
	chunks := []map[string]interface{}{
		{"type": "response.output_text.delta", "item_id": "msg-123", "content_index": 0, "delta": "Hello "},
		{"type": "response.output_text.done", "item_id": "msg-123", "content_index": 0, "text": "Hello world."},
		{"type": "response.output_text.delta", "item_id": "msg-123", "content_index": 1, "delta": "Second "},
		{"type": "response.output_item.done", "item": message, "output_index": 0},
		{"type": "response.completed", "response": createTestResponse(map[string]interface{}{"output": []interface{}{message}})},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var textEvents []string
	for _, c := range got {
		switch c.Type {
		case provider.ChunkTypeTextStart:
			textEvents = append(textEvents, "start:"+c.ID)
		case provider.ChunkTypeText:
			textEvents = append(textEvents, "delta:"+c.ID+":"+c.Text)
		case provider.ChunkTypeTextEnd:
			textEvents = append(textEvents, "end:"+c.ID)
		}
	}
	want := []string{
		"start:msg-123",
		"delta:msg-123:Hello ",
		"delta:msg-123:world.",
		"end:msg-123",
		"start:msg-123:1",
		"delta:msg-123:1:Second ",
		"delta:msg-123:1:part.",
		"end:msg-123:1",
	}
	if len(textEvents) != len(want) {
		t.Fatalf("textEvents = %v, want %v", textEvents, want)
	}
	for i := range want {
		if textEvents[i] != want[i] {
			t.Fatalf("textEvents[%d] = %q, want %q (full: %v)", i, textEvents[i], want[i], textEvents)
		}
	}
}

// TS: "emits stream failures as errors"
func TestPerplexityDoStream_EmitsFailureAsError(t *testing.T) {
	chunks := []map[string]interface{}{
		{"type": "response.failed", "sequence_number": 0, "error": map[string]interface{}{"message": "Agent run failed", "type": "server_error"}},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var sawError bool
	var finish *provider.StreamChunk
	for _, c := range got {
		if c.Type == provider.ChunkTypeError {
			sawError = true
			if c.Text != "Agent run failed" {
				t.Fatalf("error text = %q, want Agent run failed", c.Text)
			}
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if !sawError {
		t.Fatal("expected an error chunk")
	}
	if finish == nil || finish.FinishReason != types.FinishReasonError || finish.RawFinishReason != "failed" {
		t.Fatalf("finish = %+v, want error/failed", finish)
	}
}

// TS: "recovers text from %s without deltas" (response.output_text.done,
// response.output_item.done, response.completed all recover the full text as
// a single delta when no incremental deltas preceded them).
func TestPerplexityDoStream_RecoversTextWithoutDeltas(t *testing.T) {
	response := createTestResponse(map[string]interface{}{"output": []interface{}{createTestResponse(nil)["output"].([]interface{})[1]}})
	for _, eventType := range []string{"response.output_text.done", "response.output_item.done", "response.completed"} {
		t.Run(eventType, func(t *testing.T) {
			item := response["output"].([]interface{})[0]
			chunks := []map[string]interface{}{
				{
					"type": eventType, "item_id": "msg-123", "output_index": 0, "content_index": 0,
					"text": "Hello from Perplexity.", "item": item, "response": response,
				},
			}
			_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))
			stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
			if err != nil {
				t.Fatalf("DoStream() error = %v", err)
			}
			defer func() { _ = stream.Close() }()
			got := collectChunks(t, stream)

			var events []string
			for _, c := range got {
				switch c.Type {
				case provider.ChunkTypeTextStart:
					events = append(events, "start:"+c.ID)
				case provider.ChunkTypeText:
					events = append(events, "delta:"+c.ID+":"+c.Text)
				case provider.ChunkTypeTextEnd:
					events = append(events, "end:"+c.ID)
				}
			}
			want := []string{"start:msg-123", "delta:msg-123:Hello from Perplexity.", "end:msg-123"}
			if len(events) != len(want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
			for i := range want {
				if events[i] != want[i] {
					t.Fatalf("events[%d] = %q, want %q (full: %v)", i, events[i], want[i], events)
				}
			}
		})
	}
}

// TS: "streams Agent API reasoning thoughts"
func TestPerplexityDoStream_ReasoningThoughts(t *testing.T) {
	chunks := []map[string]interface{}{
		{"type": "response.reasoning.started", "sequence_number": 0, "thought": "Planning. "},
		{"type": "response.reasoning.search_queries", "sequence_number": 1, "queries": []interface{}{"latest AI news"}, "thought": "Searching. "},
		{"type": "response.reasoning.search_results", "sequence_number": 2, "results": []interface{}{}, "thought": "Reviewing results. "},
		{"type": "response.reasoning.fetch_url_queries", "sequence_number": 3, "urls": []interface{}{"https://example.com/source"}, "thought": "Fetching details. "},
		{"type": "response.reasoning.fetch_url_results", "sequence_number": 4, "contents": []interface{}{}, "thought": "Checking details. "},
		{"type": "response.reasoning.stopped", "sequence_number": 5, "thought": "Done."},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))
	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var thoughts string
	var sawStart, sawEnd bool
	for _, c := range got {
		switch c.Type {
		case provider.ChunkTypeReasoningStart:
			sawStart = true
			if c.ID != "reasoning-0" {
				t.Fatalf("reasoning id = %q, want reasoning-0", c.ID)
			}
		case provider.ChunkTypeReasoning:
			thoughts += c.Reasoning
		case provider.ChunkTypeReasoningEnd:
			sawEnd = true
		}
	}
	if !sawStart || !sawEnd {
		t.Fatalf("reasoning start/end missing: start=%v end=%v", sawStart, sawEnd)
	}
	want := "Planning. Searching. Reviewing results. Fetching details. Checking details. Done."
	if thoughts != want {
		t.Fatalf("thoughts = %q, want %q", thoughts, want)
	}
}

// TS: "deduplicates a URL across annotations, search results, and fetched contents"
func TestPerplexityDoStream_DeduplicatesAcrossSources(t *testing.T) {
	searchResult := map[string]interface{}{"id": 7, "title": "Search result", "url": "https://example.com/source", "snippet": "Search result content."}
	message := map[string]interface{}{
		"type": "message",
		"content": []interface{}{
			map[string]interface{}{"type": "output_text", "text": "Answer", "annotations": []interface{}{
				map[string]interface{}{"type": "url_citation", "url": "https://example.com/source"},
			}},
		},
	}
	chunks := []map[string]interface{}{
		{"type": "response.output_item.done", "item": message},
		{"type": "response.reasoning.search_results", "results": []interface{}{searchResult}},
		{"type": "response.reasoning.fetch_url_results", "contents": []interface{}{searchResult}},
		{"type": "response.completed", "response": createTestResponse(map[string]interface{}{
			"output": []interface{}{
				map[string]interface{}{"type": "search_results", "results": []interface{}{searchResult}},
				message,
			},
		})},
	}
	_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))
	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	var sources []*provider.StreamChunk
	for _, c := range got {
		if c.Type == provider.ChunkTypeSource {
			sources = append(sources, c)
		}
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want 1 (deduped by URL)", len(sources))
	}
	if sources[0].SourceContent.ID != "7" {
		t.Fatalf("source id = %q, want 7", sources[0].SourceContent.ID)
	}
}

// TS convertPerplexityUsage.test.ts: "treats reasoning tokens as separate from completion tokens"
func TestPerplexityConvertUsage_ReasoningTokensAreSubsetOfOutput(t *testing.T) {
	usage := &perplexityUsage{
		InputTokens:         33,
		OutputTokens:        205342,
		OutputTokensDetails: &perplexityOutputTokensDetails{ReasoningTokens: int64Ptr(193947)},
	}
	got := convertPerplexityUsage(usage, nil)
	if *got.InputTokens != 33 || *got.InputDetails.NoCacheTokens != 33 {
		t.Fatalf("input = %+v", got)
	}
	if *got.OutputTokens != 205342 || *got.OutputDetails.TextTokens != 11395 || *got.OutputDetails.ReasoningTokens != 193947 {
		t.Fatalf("output = %+v, want total=205342 text=11395 reasoning=193947", got.OutputDetails)
	}
}

func int64Ptr(v int64) *int64 { return &v }

// TS: "preserves citation annotations from output items and %s" (it.each over
// response.completed / response.incomplete). A message with a citation is
// first surfaced via response.output_item.done (so its source is already
// emitted), then reappears in the terminal event's response.output[] -- the
// terminal event must not re-emit or duplicate it, while a second message's
// citation (only present in the terminal event) is still emitted once.
func TestPerplexityDoStream_PreservesCitationAnnotationsAcrossTerminalEvents(t *testing.T) {
	for _, terminalType := range []string{"response.completed", "response.incomplete"} {
		t.Run(terminalType, func(t *testing.T) {
			message := map[string]interface{}{
				"id":   "msg-123",
				"type": "message",
				"content": []interface{}{
					map[string]interface{}{
						"type": "output_text",
						"text": "A cited answer.",
						"annotations": []interface{}{
							map[string]interface{}{"type": "url_citation", "url": "https://example.com/first", "title": "First"},
							map[string]interface{}{"type": "file_citation", "file_id": "file-123"},
						},
					},
				},
			}
			status := "completed"
			if terminalType == "response.incomplete" {
				status = "incomplete"
			}
			chunks := []map[string]interface{}{
				{"type": "response.output_item.done", "item": message, "output_index": 0},
				{"type": terminalType, "response": createTestResponse(map[string]interface{}{
					"status": status,
					"output": []interface{}{
						message,
						map[string]interface{}{
							"type": "message",
							"content": []interface{}{
								map[string]interface{}{
									"type": "output_text",
									"text": "Another citation.",
									"annotations": []interface{}{
										map[string]interface{}{"type": "url_citation", "url": "https://example.com/second", "title": "Second"},
									},
								},
							},
						},
					},
				})},
			}
			_, model := newPerplexityTestServer(t, streamHandler(t, chunks, nil))
			stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}})
			if err != nil {
				t.Fatalf("DoStream() error = %v", err)
			}
			defer func() { _ = stream.Close() }()
			got := collectChunks(t, stream)

			var sources []*provider.StreamChunk
			for _, c := range got {
				if c.Type == provider.ChunkTypeSource {
					sources = append(sources, c)
				}
			}
			if len(sources) != 2 {
				t.Fatalf("sources = %d, want 2 (First, Second), got %+v", len(sources), sources)
			}
			if sources[0].SourceContent.URL != "https://example.com/first" || sources[0].SourceContent.Title != "First" {
				t.Fatalf("sources[0] = %+v, want First", sources[0].SourceContent)
			}
			if sources[1].SourceContent.URL != "https://example.com/second" || sources[1].SourceContent.Title != "Second" {
				t.Fatalf("sources[1] = %+v, want Second", sources[1].SourceContent)
			}
		})
	}
}

// TS: "preserves native tool traces in raw chunks without rejecting the response"
func TestPerplexityDoStream_PreservesNativeToolTracesInRawChunks(t *testing.T) {
	financeOutput := map[string]interface{}{
		"type":       "finance_results",
		"categories": []interface{}{"quote"},
		"tickers":    []interface{}{"AAPL"},
		"results": []interface{}{
			map[string]interface{}{"category": "quote", "content": "AAPL: $230.00", "sources": []interface{}{"https://example.com/quote"}, "tickers": []interface{}{"AAPL"}},
		},
	}
	firstEvent := map[string]interface{}{"type": "response.output_item.done", "output_index": 0, "item": financeOutput}
	base := createTestResponse(nil)
	output := append([]interface{}{financeOutput}, base["output"].([]interface{})...)
	events := append([]map[string]interface{}{firstEvent}, createTestStreamChunks(map[string]interface{}{"output": output})...)

	_, model := newPerplexityTestServer(t, streamHandler(t, events, nil))

	stream, err := model.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: testPrompt}, IncludeRawChunks: true})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got := collectChunks(t, stream)

	for _, c := range got {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %+v", c)
		}
	}

	var sawRawFirstEvent bool
	for _, c := range got {
		if c.Type == provider.ChunkTypeRaw {
			b, _ := json.Marshal(c.Raw)
			want, _ := json.Marshal(firstEvent)
			if string(b) == string(want) {
				sawRawFirstEvent = true
			}
		}
	}
	if !sawRawFirstEvent {
		t.Fatal("expected a raw chunk carrying the unhandled finance_results output_item.done event")
	}

	var finish *provider.StreamChunk
	for _, c := range got {
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.RawFinishReason != "completed" {
		t.Fatalf("finish = %+v, want stop/completed", finish)
	}
}
