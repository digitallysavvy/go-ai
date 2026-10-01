package cohere

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestCohereRerankingModelMetadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewRerankingModel(p, "rerank-v3.5")
	if m.SpecificationVersion() != "v3" || m.Provider() != "cohere" || m.ModelID() != "rerank-v3.5" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
}

func TestCohereRerankingModelDoRerank(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	p := newCohereProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"rr1","results":[{"index":1,"relevance_score":0.92},{"index":0,"relevance_score":0.51}]}`)),
		}, nil
	})
	m := NewRerankingModel(p, "rerank-v3.5")

	topN := 2
	result, err := m.DoRerank(context.Background(), &provider.RerankOptions{
		Query:     "hello",
		Documents: []string{"doc0", "doc1"},
		TopN:      &topN,
	})
	if err != nil {
		t.Fatalf("DoRerank error = %v", err)
	}
	if seenPath != "/rerank" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["model"] != "rerank-v3.5" || seenBody["query"] != "hello" {
		t.Fatalf("request mismatch: %#v", seenBody)
	}
	if len(result.Ranking) != 2 || result.Ranking[0].Index != 1 || result.Ranking[0].RelevanceScore != 0.92 {
		t.Fatalf("ranking mismatch: %#v", result.Ranking)
	}
	if result.Response.ID != "rr1" || result.Response.ModelID != "rerank-v3.5" {
		t.Fatalf("response metadata mismatch: %#v", result.Response)
	}
}

func TestCohereRerankingModelBuildRequestBodyAndError(t *testing.T) {
	m := NewRerankingModel(New(Config{APIKey: "k"}), "rerank-v3.5")
	topN := 3
	body := m.buildRequestBody(&provider.RerankOptions{
		Query:     "q",
		Documents: []map[string]interface{}{{"text": "a"}},
		TopN:      &topN,
	})
	if body["top_n"] != 3 {
		t.Fatalf("top_n = %#v", body["top_n"])
	}
	if _, ok := body["documents"].([]map[string]interface{}); !ok {
		t.Fatalf("documents type mismatch: %#v", body["documents"])
	}

	err := m.handleError(errors.New("boom"))
	if !providererrors.IsProviderError(err) {
		t.Fatalf("expected provider error, got %T", err)
	}
}

type cohereRoundTripper func(*http.Request) (*http.Response, error)

func (f cohereRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newCohereProviderWithTransport(t *testing.T, rt cohereRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://cohere.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://cohere.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}
