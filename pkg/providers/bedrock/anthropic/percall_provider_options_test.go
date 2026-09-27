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

// TestPerCallProviderOptions_BedrockCustomKey verifies Bedrock-Anthropic gets
// per-call provider options for free from the shared
// pkg/providers/anthropic.LanguageModel it wraps: TS derives
// providerOptionsName "bedrock" from config.provider =
// "bedrock.anthropic.messages" (getProviderOptionsName splits on the first
// '.'), so providerOptions.bedrock.effort must flow through on every call,
// not just via construction-time ModelOptions.
func TestPerCallProviderOptions_BedrockCustomKey(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-opus-4-5-v1:0")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"bedrock": map[string]interface{}{
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

// TestPerCallProviderOptions_BedrockCanonicalAnthropicKey verifies the
// canonical "anthropic" providerOptions key still works for Bedrock-Anthropic
// alongside the custom "bedrock" key (TS parseProviderOptions always checks
// the canonical key too).
func TestPerCallProviderOptions_BedrockCanonicalAnthropicKey(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-opus-4-5-v1:0")
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

// TestPerCallProviderOptions_BedrockCustomKeyWinsOverCanonical verifies the
// custom "bedrock" key takes precedence over the canonical "anthropic" key
// when both set the same field, matching TS's
// Object.assign({}, canonicalOptions, customProviderOptions) merge order.
func TestPerCallProviderOptions_BedrockCustomKeyWinsOverCanonical(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-opus-4-5-v1:0")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"effort": "low"},
			"bedrock":   map[string]interface{}{"effort": "high"},
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
