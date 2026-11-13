package alibaba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR97provbFractionalUsageRawExact: TS usage counts are z.number() and
// convertAlibabaUsage keeps `raw: usage`, so a fractional count stays exact
// in Usage.Raw (the int fields truncate it; known difference "Fractional
// counts in integer fields"). Go's raw held the truncated retry value.
func TestR97provbFractionalUsageRawExact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10.5,"completion_tokens":3,"total_tokens":13.5,"prompt_tokens_details":{"cached_tokens":1.5}}}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "qwen-plus")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if *res.Usage.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want 10", *res.Usage.InputTokens)
	}
	details, _ := res.Usage.Raw["prompt_tokens_details"].(map[string]interface{})
	if res.Usage.Raw["prompt_tokens"] != 10.5 || details["cached_tokens"] != 1.5 {
		t.Fatalf("raw = %#v, want the exact fractional counts", res.Usage.Raw)
	}
}
