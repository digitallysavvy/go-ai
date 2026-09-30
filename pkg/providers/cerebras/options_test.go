package cerebras

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported (behaviorally) from ai/packages/cerebras/src/cerebras-provider.ts's
// transformCerebrasRequestBody.

func TestCerebras_MaxTokensRenamedToMaxCompletionTokens(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	maxTokens := 256
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		MaxTokens: &maxTokens,
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if _, hasMaxTokens := captured["max_tokens"]; hasMaxTokens {
		t.Fatalf("expected max_tokens to be renamed away, got body %#v", captured)
	}
	if captured["max_completion_tokens"] != float64(256) {
		t.Fatalf("max_completion_tokens = %#v, want 256", captured["max_completion_tokens"])
	}
}

func TestCerebras_ProviderOptionsForwardedToWire(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"cerebras": map[string]interface{}{
				"user":              "user-123",
				"parallelToolCalls": false,
				"logprobs":          true,
				"topLogprobs":       3,
				"serviceTier":       "flex",
				"reasoningEffort":   "high",
				"reasoningFormat":   "raw",
				"promptCacheKey":    "cache-key",
			},
		},
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if captured["user"] != "user-123" {
		t.Errorf("user = %#v", captured["user"])
	}
	if captured["parallel_tool_calls"] != false {
		t.Errorf("parallel_tool_calls = %#v", captured["parallel_tool_calls"])
	}
	if captured["logprobs"] != true {
		t.Errorf("logprobs = %#v", captured["logprobs"])
	}
	if captured["top_logprobs"] != float64(3) {
		t.Errorf("top_logprobs = %#v", captured["top_logprobs"])
	}
	if captured["service_tier"] != "flex" {
		t.Errorf("service_tier = %#v", captured["service_tier"])
	}
	if captured["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %#v", captured["reasoning_effort"])
	}
	if captured["reasoning_format"] != "raw" {
		t.Errorf("reasoning_format = %#v", captured["reasoning_format"])
	}
	if captured["prompt_cache_key"] != "cache-key" {
		t.Errorf("prompt_cache_key = %#v", captured["prompt_cache_key"])
	}
}

func TestCerebras_StrictJSONSchemaAppliedToResponseFormat(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "test-key", BaseURL: server.URL}).LanguageModel("gpt-oss-120b")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: map[string]interface{}{"type": "object"},
			Name:   "Output",
		},
		ProviderOptions: map[string]interface{}{
			"cerebras": map[string]interface{}{
				"strictJsonSchema": true,
			},
		},
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	responseFormat, ok := captured["response_format"].(map[string]interface{})
	if !ok {
		t.Fatalf("response_format type = %T", captured["response_format"])
	}
	jsonSchema, ok := responseFormat["json_schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("json_schema type = %T", responseFormat["json_schema"])
	}
	if jsonSchema["strict"] != true {
		t.Fatalf("json_schema.strict = %#v, want true", jsonSchema["strict"])
	}
}
