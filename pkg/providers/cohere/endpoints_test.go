package cohere

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Chat, embed and rerank all use the v2 API under a host-only base URL: embed
// went to /v1/v1/embed and rerank to /v1/rerank before.
func TestEndpointsUseV2(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/embed":
			_, _ = w.Write([]byte(`{"embeddings":{"float":[[0.1,0.2]]},"meta":{"billed_units":{"input_tokens":1}}}`))
		case "/v2/rerank":
			_, _ = w.Write([]byte(`{"id":"r","results":[{"index":0,"relevance_score":0.5}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	em, err := p.EmbeddingModel("embed-v4.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := em.DoEmbed(context.Background(), "hi", nil); err != nil {
		t.Fatalf("embed: %v", err)
	}
	rm, err := p.RerankingModel("rerank-v3.5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rm.DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}}); err != nil {
		t.Fatalf("rerank: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[0] != "/v2/embed" || paths[1] != "/v2/rerank" {
		t.Fatalf("paths = %v", paths)
	}
	if DefaultBaseURL != "https://api.cohere.com/v2" {
		t.Fatalf("DefaultBaseURL = %q", DefaultBaseURL)
	}
}
