package together

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports packages/togetherai/src/reranking/togetherai-reranking-model.ts
// (ai@7.0.118) request/response mapping.

func TestTogetherRerankingModel_RequestBodyAndResponse(t *testing.T) {
	var gotBody map[string]interface{}
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "req-1",
			"model": "Salesforce/Llama-Rank-v1",
			"results": [
				{"index": 1, "relevance_score": 0.95},
				{"index": 0, "relevance_score": 0.42}
			],
			"usage": {"prompt_tokens": 10, "completion_tokens": 0, "total_tokens": 10}
		}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.RerankingModel("Salesforce/Llama-Rank-v1")
	if err != nil {
		t.Fatalf("RerankingModel() error: %v", err)
	}

	topN := 2
	result, err := model.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "capital of France",
		Documents: []string{"Paris is the capital of France.", "Berlin is in Germany."},
		TopN:      &topN,
	})
	if err != nil {
		t.Fatalf("DoRerank() error: %v", err)
	}

	if gotPath != "/v1/rerank" {
		t.Errorf("path = %q, want /v1/rerank", gotPath)
	}
	if gotBody["model"] != "Salesforce/Llama-Rank-v1" {
		t.Errorf("model = %v, want Salesforce/Llama-Rank-v1", gotBody["model"])
	}
	if gotBody["query"] != "capital of France" {
		t.Errorf("query = %v", gotBody["query"])
	}
	if gotBody["top_n"] != float64(2) {
		t.Errorf("top_n = %v, want 2", gotBody["top_n"])
	}
	if gotBody["return_documents"] != false {
		t.Errorf("return_documents = %v, want false", gotBody["return_documents"])
	}
	if _, ok := gotBody["rank_fields"]; ok {
		t.Errorf("rank_fields should be omitted when not provided, got %v", gotBody["rank_fields"])
	}
	docs, ok := gotBody["documents"].([]interface{})
	if !ok || len(docs) != 2 {
		t.Fatalf("documents = %#v, want 2 string documents", gotBody["documents"])
	}

	if len(result.Ranking) != 2 {
		t.Fatalf("ranking length = %d, want 2", len(result.Ranking))
	}
	if result.Ranking[0].Index != 1 || result.Ranking[0].RelevanceScore != 0.95 {
		t.Errorf("ranking[0] = %#v, want {Index:1 RelevanceScore:0.95}", result.Ranking[0])
	}
	if result.Ranking[1].Index != 0 || result.Ranking[1].RelevanceScore != 0.42 {
		t.Errorf("ranking[1] = %#v, want {Index:0 RelevanceScore:0.42}", result.Ranking[1])
	}
	if result.Response.ID != "req-1" {
		t.Errorf("response.ID = %q, want req-1", result.Response.ID)
	}
	if result.Response.ModelID != "Salesforce/Llama-Rank-v1" {
		t.Errorf("response.ModelID = %q", result.Response.ModelID)
	}
}

// TestTogetherRerankingModel_RankFieldsProviderOption ports the rankFields
// providerOptions field (togetheraiRerankingModelOptionsSchema).
func TestTogetherRerankingModel_RankFieldsProviderOption(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [{"index": 0, "relevance_score": 0.5}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewRerankingModel(p, "mixedbread-ai/Mxbai-Rerank-Large-V2")

	_, err := model.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "q",
		Documents: []map[string]interface{}{{"title": "a", "text": "b"}},
		ProviderOptions: map[string]interface{}{
			"together": map[string]interface{}{
				"rankFields": []interface{}{"title", "text"},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoRerank() error: %v", err)
	}

	rankFields, ok := gotBody["rank_fields"].([]interface{})
	if !ok || len(rankFields) != 2 || rankFields[0] != "title" || rankFields[1] != "text" {
		t.Errorf("rank_fields = %#v, want [title text]", gotBody["rank_fields"])
	}
	if _, ok := gotBody["top_n"]; ok {
		t.Errorf("top_n should be omitted when TopN is nil, got %v", gotBody["top_n"])
	}
}

// TestTogetherRerankingModel_ErrorResponse verifies the togetheraiErrorSchema
// {error:{message}} shape surfaces as a Go error.
func TestTogetherRerankingModel_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid model"}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewRerankingModel(p, "bad-model")

	_, err := model.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "q",
		Documents: []string{"a"},
	})
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
}
