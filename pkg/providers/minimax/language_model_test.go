package minimax

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from minimax-reasoning.test.ts "sends thinking via the minimax
// provider option and returns a reasoning part". MiniMax-specific behavior:
// the provider delegates to the Anthropic message protocol, so `thinking`
// must flow through the `minimax` provider-options namespace to the
// request, and the response `thinking` block must surface as a reasoning
// part.
func TestDoGenerateSendsThinkingViaMiniMaxProviderOption(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_minimax_reasoning",
			"type": "message",
			"role": "assistant",
			"content": [
				{"type": "thinking", "thinking": "Counting the letters...", "signature": "sig_123"},
				{"type": "text", "text": "There are 3 \"r\"s."}
			],
			"model": "minimax-m3",
			"stop_reason": "end_turn",
			"stop_sequence": null,
			"usage": {"input_tokens": 4, "output_tokens": 30}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	model := NewLanguageModel(p, "minimax-m3")

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		}},
		ProviderOptions: map[string]interface{}{
			"minimax": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "adaptive"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	// The thinking option flows to the request under the Anthropic contract.
	if capturedBody["model"] != "minimax-m3" {
		t.Fatalf("model = %v, want minimax-m3", capturedBody["model"])
	}
	thinking, ok := capturedBody["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("thinking = %#v, want {type: adaptive}", capturedBody["thinking"])
	}

	// The thinking block surfaces as a reasoning content part (Go keeps text
	// separate on result.Text rather than in a unified content array).
	if len(result.Content) != 1 {
		t.Fatalf("Content = %#v, want 1 reasoning part", result.Content)
	}
	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok || rc.Text != "Counting the letters..." || rc.Signature != "sig_123" {
		t.Fatalf("Content[0] = %#v", result.Content[0])
	}
	if result.Text != `There are 3 "r"s.` {
		t.Fatalf("Text = %q", result.Text)
	}
}

// TestDoGenerateDoesNotNarrowThinkingType verifies Go matches TS parity: TS's
// MiniMaxLanguageModelOptions type narrows thinking.type to
// "adaptive"|"disabled" for compile-time TypeScript ergonomics only —
// minimax-provider.ts constructs AnthropicLanguageModel directly and never
// imports or validates against that narrower schema at runtime, and
// minimax-reasoning.test.ts never asserts rejection of `thinking: { type:
// "enabled" }`. So a MiniMax call using the fuller Anthropic thinking enum
// (e.g. "enabled", which plain Anthropic also allows) must be forwarded
// as-is, not rejected.
func TestDoGenerateDoesNotNarrowThinkingType(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant",
			"content": [{"type": "text", "text": "ok"}],
			"model": "minimax-m3", "stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	model := NewLanguageModel(p, "minimax-m3")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"minimax": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": float64(2048)},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v, want thinking.type=enabled to be forwarded, not rejected", err)
	}
	thinking, ok := capturedBody["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(2048) {
		t.Fatalf("thinking = %#v, want {type: enabled, budget_tokens: 2048}", capturedBody["thinking"])
	}
}

// Verifies providerOptions.anthropic.thinking still works as the canonical
// key (TS parseProviderOptions also parses the canonical 'anthropic' key,
// with the custom 'minimax' key taking precedence when both are set).
func TestDoGenerateAcceptsCanonicalAnthropicThinkingKey(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant",
			"content": [{"type": "text", "text": "ok"}],
			"model": "minimax-m3", "stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	model := NewLanguageModel(p, "minimax-m3")

	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "disabled"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	thinking, ok := capturedBody["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v, want {type: disabled}", capturedBody["thinking"])
	}
}
