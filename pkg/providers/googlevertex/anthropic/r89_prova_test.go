package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR89provaCallHeadersWinOverConfigHeaders: TS combines the provider
// headers ({Authorization: Bearer <token>, ...options.headers}) below the
// call headers and the computed betas, and reads a configured anthropic-beta
// as user-supplied betas (anthropic-language-model.ts getHeaders /
// getBetasFromHeaders; google-vertex-anthropic-provider-node.ts). Expected
// headers recorded with TS AnthropicLanguageModel at 382d0b8eaf.
func TestR89provaCallHeadersWinOverConfigHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	p := New(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "tok", nil },
		Headers:    map[string]string{"anthropic-beta": "cfg-beta", "x-custom": "cfg", "x-both": "cfg"},
		HTTPClient: srv.Client(),
	})
	m, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		Headers:         map[string]string{"x-both": "call", "authorization": "Bearer call"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": map[string]interface{}{"type": "adaptive", "display": "updates"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"Anthropic-Beta": "thinking-display-updates-2026-08-18,cfg-beta",
		"Authorization":  "Bearer call",
		"X-Both":         "call",
		"X-Custom":       "cfg",
	} {
		if got.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want)
		}
	}
}

// TestR89provaLabelsHeaderPrecedence: TS transformRequestHeaders sees the
// provider headers combined below the call headers, so a call labels header
// beats a provider one, and a provider one beats the header generated from
// providerOptions.vertex.labels (google-vertex-anthropic-provider.ts).
func TestR89provaLabelsHeaderPrecedence(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	labels := map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]interface{}{"team": "x"}}}
	run := func(opts Options, callHeaders map[string]string) string {
		t.Helper()
		opts.BaseURL, opts.HTTPClient = srv.URL, srv.Client()
		opts.AuthToken = func(context.Context) (string, error) { return "tok", nil }
		m, err := New(opts).LanguageModel("claude-sonnet-4-5")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Headers: callHeaders, ProviderOptions: labels}); err != nil {
			t.Fatal(err)
		}
		if got.Get(generatedLabelsHeader) != "" {
			t.Fatalf("marker header sent")
		}
		return got.Get(LabelsHeader)
	}
	if v := run(Options{Headers: map[string]string{LabelsHeader: "provider"}}, map[string]string{LabelsHeader: "call"}); v != "call" {
		t.Errorf("call vs provider: %q", v)
	}
	if v := run(Options{Headers: map[string]string{LabelsHeader: "provider"}}, nil); v != "provider" {
		t.Errorf("provider vs generated: %q", v)
	}
	resolver := func(context.Context) (map[string]string, error) { return map[string]string{LabelsHeader: "resolved"}, nil }
	if v := run(Options{HeadersResolver: resolver}, nil); v != "resolved" {
		t.Errorf("resolver vs generated: %q", v)
	}
	if v := run(Options{HeadersResolver: resolver}, map[string]string{LabelsHeader: "call"}); v != "call" {
		t.Errorf("call vs resolver: %q", v)
	}
	if v := run(Options{}, nil); v != "eyJ0ZWFtIjoieCJ9" {
		t.Errorf("generated: %q", v)
	}
}
