package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS betas: new Set([...betas, ...toolsBetas, ...userSuppliedBetas,
// ...anthropicBeta]) where userSuppliedBetas is getBetasFromHeaders(config
// headers, call headers): a configured anthropic-beta comes before the call
// header's and the anthropicBeta option's betas (anthropic-language-model.ts
// 1436-1442, 1489-1504). Go appended the configured betas last (prova r89
// fix 10 approximation).
func TestR89integConfigBetasOrder(t *testing.T) {
	var mu sync.Mutex
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Get("anthropic-beta")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	m, err := New(Options{
		BaseURL:    srv.URL,
		AuthToken:  func(context.Context) (string, error) { return "tok", nil },
		Headers:    map[string]string{"Anthropic-Beta": "cfg-beta"},
		HTTPClient: srv.Client(),
	}).LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		Headers:         map[string]string{"anthropic-beta": "call-beta"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"opt-beta"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got != "cfg-beta,call-beta,opt-beta" {
		t.Fatalf("anthropic-beta = %q, want cfg-beta,call-beta,opt-beta", got)
	}
}
