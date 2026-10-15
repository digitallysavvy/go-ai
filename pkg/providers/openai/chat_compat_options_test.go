package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS OpenAICompatibleChatLanguageModel reads <providerName> options (not
// "openai"), applies user/reasoningEffort/textVerbosity and passes unknown
// keys straight into the request body.
func TestChatCompatReadsProviderNamedOptions(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "my-host", ChatProviderName: "my-host.chat"})
	m, _ := p.ChatModel("m")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: chatTestPrompt(),
		ProviderOptions: map[string]interface{}{"myHost": map[string]interface{}{
			"user": "u1", "reasoningEffort": "low", "textVerbosity": "high", "top_k": 5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["user"] != "u1" || body["reasoning_effort"] != "low" || body["verbosity"] != "high" {
		t.Fatalf("named options not applied: %v", body)
	}
	if body["top_k"] != float64(5) {
		t.Fatalf("unknown option not passed through: %v", body)
	}
	if _, leaked := body["reasoningEffort"]; leaked {
		t.Fatalf("known option leaked as raw key: %v", body)
	}
}

// TS OpenAICompatibleChatLanguageModel passes unmodeled provider options to
// the wire verbatim (camelCase); it does not also derive the OpenAI-only
// snake_case fields, and has no reasoning-model/service-tier capability logic.
func TestChatCompatDoesNotConvertUnmodeledOptions(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "my-host", ChatProviderName: "my-host.chat"})
	m, _ := p.ChatModel("o3-mini")
	temp := 0.5
	maxTokens := 20
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:      chatTestPrompt(),
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		ProviderOptions: map[string]interface{}{"myHost": map[string]interface{}{
			"logprobs": true, "promptCacheKey": "k1", "serviceTier": "flex", "maxCompletionTokens": 5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"top_logprobs", "prompt_cache_key", "service_tier", "max_completion_tokens"} {
		if _, ok := body[k]; ok {
			t.Errorf("unexpected snake_case field %s in %v", k, body)
		}
	}
	if body["promptCacheKey"] != "k1" || body["serviceTier"] != "flex" || body["logprobs"] != true {
		t.Errorf("passthrough keys missing: %v", body)
	}
	if body["temperature"] != 0.5 || body["max_tokens"] != float64(20) {
		t.Errorf("reasoning-model gating applied to compat wrapper: %v", body)
	}
}
