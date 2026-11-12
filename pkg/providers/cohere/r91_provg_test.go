package cohere

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provg2-05: a missing API key is the LoadAPIKeyError itself (TS loadApiKey),
// not a wrapping ProviderError.
func TestR91provgMissingAPIKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("COHERE_API_KEY", "")
	em, _ := New(Config{}).EmbeddingModel("embed-v4.0")
	_, err := em.DoEmbed(context.Background(), "hi", nil)
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("error = %T %v", err, err)
	}
}

func r91provgCapture(t *testing.T, resp string) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// Typed-option ruling probe: Go-typed option literals reach the request like
// their JSON form (chat thinking, rerank options).
func TestR91provgTypedOptionLiterals(t *testing.T) {
	srv, got := r91provgCapture(t, `{"id":"x","finish_reason":"COMPLETE","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"usage":{"billed_units":{"input_tokens":1,"output_tokens":1},"tokens":{"input_tokens":1,"output_tokens":1}}}`)
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("command-a-reasoning-08-2025")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{
		"cohere": map[string]map[string]interface{}{"thinking": {"type": "enabled", "tokenBudget": int64(100)}},
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(*got), `"thinking":{"token_budget":100,"type":"enabled"}`) {
		t.Fatalf("chat body = %s", *got)
	}
	srv2, got2 := r91provgCapture(t, `{"id":"x","results":[{"index":0,"relevance_score":0.5}],"meta":{}}`)
	rm, _ := New(Config{APIKey: "k", BaseURL: srv2.URL}).RerankingModel("rerank-v3.5")
	if _, err := rm.DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}, ProviderOptions: map[string]interface{}{"cohere": map[string]int{"maxTokensPerDoc": 5}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(*got2), `"max_tokens_per_doc":5`) {
		t.Fatalf("rerank body = %s", *got2)
	}
}
