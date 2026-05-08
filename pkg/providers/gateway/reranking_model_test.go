package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestProvider_RerankingModel_CreatesGatewayRerankingModel(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	model, err := p.RerankingModel("cohere/rerank-v3.5")
	if err != nil {
		t.Fatalf("RerankingModel error = %v", err)
	}
	if model.Provider() != "gateway" {
		t.Fatalf("Provider() = %q, want gateway", model.Provider())
	}
	if model.ModelID() != "cohere/rerank-v3.5" {
		t.Fatalf("ModelID() = %q, want cohere/rerank-v3.5", model.ModelID())
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
	}
}

func TestRerankingModel_DoRerank_RequestAndResponseParity(t *testing.T) {
	var path string
	var headers http.Header
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		headers = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"ranking":[
				{"index":0,"relevanceScore":0.89},
				{"index":2,"relevanceScore":0.15}
			],
			"providerMetadata":{"gateway":{"routing":{"provider":"cohere"}}}
		}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.RerankingModel("cohere/rerank-v3.5")
	if err != nil {
		t.Fatalf("RerankingModel error = %v", err)
	}

	topN := 2
	result, err := model.DoRerank(context.Background(), &provider.RerankOptions{
		Documents: map[string]interface{}{
			"type":   "text",
			"values": []string{"Paris", "Berlin", "Madrid"},
		},
		Query: "capital of France",
		TopN:  &topN,
		Headers: map[string]string{
			"Custom-Header": "test-value",
		},
		ProviderOptions: map[string]interface{}{
			"cohere": map[string]interface{}{"maxTokensPerDoc": 512},
		},
	})
	if err != nil {
		t.Fatalf("DoRerank error = %v", err)
	}

	if path != "/reranking-model" {
		t.Fatalf("path = %q, want /reranking-model", path)
	}
	if headers.Get("ai-reranking-model-specification-version") != "4" {
		t.Fatalf("spec header = %q, want 4", headers.Get("ai-reranking-model-specification-version"))
	}
	if headers.Get("ai-model-id") != "cohere/rerank-v3.5" {
		t.Fatalf("model header = %q, want cohere/rerank-v3.5", headers.Get("ai-model-id"))
	}
	if headers.Get("Custom-Header") != "test-value" {
		t.Fatalf("custom header = %q, want test-value", headers.Get("Custom-Header"))
	}
	if body["query"] != "capital of France" {
		t.Fatalf("query = %#v, want capital of France", body["query"])
	}
	if body["topN"] != float64(2) {
		t.Fatalf("topN = %#v, want 2", body["topN"])
	}
	if _, ok := body["providerOptions"].(map[string]interface{}); !ok {
		t.Fatalf("providerOptions missing or wrong type: %#v", body["providerOptions"])
	}
	if len(result.Ranking) != 2 || result.Ranking[0].Index != 0 || result.Ranking[0].RelevanceScore != 0.89 {
		t.Fatalf("ranking = %#v", result.Ranking)
	}
	if http.Header(result.Response.Headers).Get("x-request-id") != "req-123" {
		t.Fatalf("response header x-request-id = %q, want req-123", http.Header(result.Response.Headers).Get("x-request-id"))
	}
	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata missing")
	}
}

func TestRerankingModel_DoRerank_NilOptions(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.RerankingModel("cohere/rerank-v3.5")
	if err != nil {
		t.Fatalf("RerankingModel error = %v", err)
	}
	if _, err := model.DoRerank(context.Background(), nil); err == nil {
		t.Fatal("DoRerank should reject nil options")
	}
}
