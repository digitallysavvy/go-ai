package anthropic

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

// TestPerCallProviderOptions_VertexCustomKey verifies Google Vertex-Anthropic
// gets per-call provider options for free from the shared
// pkg/providers/anthropic.LanguageModel it wraps: TS derives
// providerOptionsName "googleVertex" from config.provider =
// "googleVertex.anthropic.messages" (getProviderOptionsName splits on the
// first '.'; see google-vertex-anthropic-provider.ts), so
// providerOptions.googleVertex.effort must flow through on every call, not
// just via construction-time ModelOptions.
func TestPerCallProviderOptions_VertexCustomKey(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewGoogleVertexAnthropicProvider(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "test-token", nil },
		HTTPClient: srv.Client(),
	})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"effort": "high",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	outputConfig, ok := capturedBody["output_config"].(map[string]interface{})
	if !ok || outputConfig["effort"] != "high" {
		t.Fatalf("output_config = %#v, want {effort: high}", capturedBody["output_config"])
	}
}

// TestPerCallProviderOptions_VertexCanonicalAnthropicKey verifies the
// canonical "anthropic" providerOptions key still works for
// Google Vertex-Anthropic alongside the custom "googleVertex" key (TS
// parseProviderOptions always checks the canonical key too).
func TestPerCallProviderOptions_VertexCanonicalAnthropicKey(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewGoogleVertexAnthropicProvider(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "test-token", nil },
		HTTPClient: srv.Client(),
	})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"serviceTier": "standard_only",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	if capturedBody["service_tier"] != "standard_only" {
		t.Fatalf("service_tier = %v, want standard_only", capturedBody["service_tier"])
	}
}

// TestPerCallProviderOptions_VertexCustomKeyWinsOverCanonical verifies the
// custom "googleVertex" key takes precedence over the canonical "anthropic"
// key when both set the same field, matching TS's
// Object.assign({}, canonicalOptions, customProviderOptions) merge order.
func TestPerCallProviderOptions_VertexCustomKeyWinsOverCanonical(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewGoogleVertexAnthropicProvider(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "test-token", nil },
		HTTPClient: srv.Client(),
	})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"anthropic":    map[string]interface{}{"effort": "low"},
			"googleVertex": map[string]interface{}{"effort": "high"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	outputConfig, ok := capturedBody["output_config"].(map[string]interface{})
	if !ok || outputConfig["effort"] != "high" {
		t.Fatalf("output_config = %#v, want {effort: high} (custom key should win)", capturedBody["output_config"])
	}
}

// TestPerCallProviderOptions_VertexCustomKeyMetadataDuplicated verifies the
// response side of the custom-provider-key feature: when the caller supplies
// providerOptions.googleVertex (not just providerOptions.anthropic),
// doGenerate's providerMetadata carries both "anthropic" and "googleVertex"
// (anthropic-language-model.ts: usedCustomProviderKey && providerOptionsName
// !== 'anthropic').
func TestPerCallProviderOptions_VertexCustomKeyMetadataDuplicated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewGoogleVertexAnthropicProvider(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "test-token", nil },
		HTTPClient: srv.Client(),
	})
	model, err := p.LanguageModel(string(ClaudeSonnet4_6))
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{"sendReasoning": true},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := result.ProviderMetadata["anthropic"]; !ok {
		t.Fatalf("providerMetadata missing 'anthropic' key: %#v", result.ProviderMetadata)
	}
	if _, ok := result.ProviderMetadata["googleVertex"]; !ok {
		t.Fatalf("providerMetadata missing 'googleVertex' key: %#v", result.ProviderMetadata)
	}
}
