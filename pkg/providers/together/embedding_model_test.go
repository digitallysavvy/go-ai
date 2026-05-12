package together

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestEmbeddingModelBasics(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewEmbeddingModel(p, "embed-model")
	if m.SpecificationVersion() != "v3" || m.Provider() != "together" || m.ModelID() != "embed-model" {
		t.Fatalf("unexpected model metadata")
	}
	if m.MaxEmbeddingsPerCall() != 2048 || !m.SupportsParallelCalls() {
		t.Fatalf("unexpected embedding capabilities")
	}
	if optsHeaders(nil) != nil {
		t.Fatalf("optsHeaders(nil) should be nil")
	}
}

func TestEmbeddingModelDoEmbedManyAndDoEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Trace"); got != "abc" {
			t.Fatalf("expected custom headers in request, got X-Trace=%q", got)
		}
		w.Header().Set("X-Req", "123")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "embedding": []float64{1.1, 2.2}, "index": 0},
				{"object": "embedding", "embedding": []float64{3.3, 4.4}, "index": 1},
			},
			"model": "embed-model",
			"usage": map[string]interface{}{"prompt_tokens": 6, "total_tokens": 6},
		})
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewEmbeddingModel(p, "embed-model")

	res, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, &provider.EmbedModelOptions{
		Headers: map[string]string{"X-Trace": "abc"},
	})
	if err != nil {
		t.Fatalf("DoEmbedMany() error = %v", err)
	}
	if len(res.Embeddings) != 2 || len(res.Embeddings[0]) != 2 {
		t.Fatalf("unexpected embeddings result: %+v", res)
	}
	if res.Usage.InputTokens != 6 || res.Usage.TotalTokens != 6 {
		t.Fatalf("unexpected usage: %+v", res.Usage)
	}
	if len(res.Responses) != 1 || res.Responses[0].Headers["X-Req"][0] != "123" {
		t.Fatalf("response headers not captured: %+v", res.Responses)
	}

	one, err := m.DoEmbed(context.Background(), "single", &provider.EmbedModelOptions{
		Headers: map[string]string{"X-Trace": "abc"},
	})
	if err != nil {
		t.Fatalf("DoEmbed() error = %v", err)
	}
	if len(one.Embedding) != 2 || one.Usage.InputTokens != 6 {
		t.Fatalf("unexpected single embedding result: %+v", one)
	}
}
