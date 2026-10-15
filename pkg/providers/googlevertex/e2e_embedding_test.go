package googlevertex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func vertexEmbeddingTestModel(t *testing.T, id string, h http.HandlerFunc) (provider.EmbeddingModel, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "tok", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.EmbeddingModel(id)
	if err != nil {
		t.Fatal(err)
	}
	return m, srv.Close
}

// TS google-vertex-embedding.json through :predict: embeddings in order, the
// token counts summed from statistics, options mapped to instances/parameters.
func TestVertexEmbeddingPredictFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "gemini", "testdata", "ts", "google-vertex-embedding.json"))
	if err != nil {
		t.Fatal(err)
	}
	var path, auth string
	var body map[string]interface{}
	m, closeFn := vertexEmbeddingTestModel(t, "text-embedding-005", func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	})
	defer closeFn()

	res, err := m.DoEmbedMany(context.Background(), []string{"sunny day at the beach", "rainy day in the city"}, &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{
			"taskType": "RETRIEVAL_DOCUMENT", "title": "weather", "outputDimensionality": float64(5), "autoTruncate": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/models/text-embedding-005:predict") || auth != "Bearer tok" {
		t.Fatalf("path=%s auth=%s", path, auth)
	}
	inst := body["instances"].([]interface{})
	first := inst[0].(map[string]interface{})
	params := body["parameters"].(map[string]interface{})
	if len(inst) != 2 || first["content"] != "sunny day at the beach" || first["task_type"] != "RETRIEVAL_DOCUMENT" || first["title"] != "weather" ||
		params["outputDimensionality"] != float64(5) || params["autoTruncate"] != false {
		t.Fatalf("body = %#v", body)
	}
	if len(res.Embeddings) != 2 || res.Embeddings[0][0] != -0.017999587580561638 || res.Embeddings[1][4] != -0.048464205116033554 {
		t.Fatalf("embeddings = %v", res.Embeddings)
	}
	if res.Usage.Tokens != 11 {
		t.Fatalf("usage = %+v, want 5+6 tokens", res.Usage)
	}
	if len(res.Responses) != 2 || res.Responses[0].Headers["Content-Type"] != "application/json" {
		t.Fatalf("responses = %+v", res.Responses)
	}
	if rawBody, ok := res.Responses[0].Body.(json.RawMessage); !ok || !strings.Contains(string(rawBody), "billableCharacterCount") {
		t.Fatalf("response body must be the raw parsed body (metadata included): %#v", res.Responses[0].Body)
	}
}

func TestVertexEmbeddingErrorsAreProviderErrors(t *testing.T) {
	m, closeFn := vertexEmbeddingTestModel(t, "text-embedding-005", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Quota exceeded for aiplatform.googleapis.com/online_prediction_requests_per_base_model","status":"RESOURCE_EXHAUSTED"}}`))
	})
	defer closeFn()
	_, err := m.DoEmbedMany(context.Background(), []string{"a"}, nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 429 || !pe.IsRetryable() {
		t.Fatalf("err = %#v", err)
	}
}

// 250 values per :predict call; 1 for gemini-embedding-2 (:embedContent).
func TestVertexEmbeddingLimitsAndEmbedContent(t *testing.T) {
	m, closeFn := vertexEmbeddingTestModel(t, "gemini-embedding-2", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-embedding-2:embedContent") {
			t.Errorf("path = %s", r.URL.Path)
		}
		cfg := b["embedContentConfig"].(map[string]interface{})
		if cfg["outputDimensionality"] != float64(3) {
			t.Errorf("embedContentConfig = %#v (top-level dimensions must be forwarded)", cfg)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]},"usageMetadata":{"promptTokenCount":7}}`))
	})
	defer closeFn()
	dims := 3
	res, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Dimensions: &dims})
	if err != nil || len(res.Embedding) != 3 || res.Usage.Tokens != 7 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	_, err = m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	var tooMany *providererrors.TooManyEmbeddingValuesForCallError
	if !errors.As(err, &tooMany) || tooMany.MaxEmbeddingsPerCall != 1 {
		t.Fatalf("err = %v", err)
	}
}
