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
