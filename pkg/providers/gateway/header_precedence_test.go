package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS combineHeaders(config, options.headers, model headers, o11y): the model's
// own headers and the o11y headers win over a per-call header of the same name,
// for every Gateway model type and the batch surface.
func TestGatewayModelAndO11yHeadersWinOverCallHeaders(t *testing.T) {
	t.Setenv("VERCEL_ENV", "production")
	var mu sync.Mutex
	seen := map[string]http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	call := map[string]string{"X-Call": "kept", "ai-o11y-environment": "spoofed", "ai-model-id": "spoofed"}
	ctx := context.Background()

	_, _ = NewEmbeddingModel(p, "openai/e").DoEmbed(ctx, "hi", &provider.EmbedModelOptions{Headers: call})
	_, _ = NewRerankingModel(p, "cohere/r").DoRerank(ctx, &provider.RerankOptions{Query: "q", Documents: []string{"a"}, Headers: call})
	_, _ = NewSpeechModel(p, "openai/s").DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi", Headers: call})
	b := p.ExperimentalBatch()
	_, _ = b.DoStartBatch(ctx, provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{testBatchTextRequest("a", "openai/gpt-5")}, Headers: call})
	_, _ = b.DoGetBatchStatus(ctx, provider.BatchV4OperationOptions{BatchID: "b1", Headers: call})

	wantModel := map[string]string{
		"/embedding-model": "openai/e", "/reranking-model": "cohere/r", "/speech-model": "openai/s",
		"/batch/start": "openai/gpt-5",
	}
	for path, h := range seen {
		if h.Get("X-Call") != "kept" {
			t.Errorf("%s: per-call header dropped: %v", path, h)
		}
		if h.Get("ai-o11y-environment") != "production" {
			t.Errorf("%s: o11y header overridden by call header: %q", path, h.Get("ai-o11y-environment"))
		}
		if want, ok := wantModel[path]; ok && h.Get("ai-model-id") != want {
			t.Errorf("%s: ai-model-id = %q, want %q (model header must win)", path, h.Get("ai-model-id"), want)
		}
	}
	if len(seen) < 5 {
		t.Fatalf("expected 5 distinct requests, saw %d: %v", len(seen), seen)
	}
}
