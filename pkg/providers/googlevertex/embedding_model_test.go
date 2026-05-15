package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVertexEmbeddingModel_MetadataAndCapabilities(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     "https://example.test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m := NewEmbeddingModel(p, "text-embedding-005")

	if got := m.SpecificationVersion(); got != "v3" {
		t.Fatalf("SpecificationVersion() = %q, want v3", got)
	}
	if got := m.Provider(); got != "google-vertex" {
		t.Fatalf("Provider() = %q, want google-vertex", got)
	}
	if got := m.ModelID(); got != "text-embedding-005" {
		t.Fatalf("ModelID() = %q", got)
	}
	if got := m.MaxEmbeddingsPerCall(); got != 2048 {
		t.Fatalf("MaxEmbeddingsPerCall() = %d, want 2048", got)
	}
	if !m.SupportsParallelCalls() {
		t.Fatal("SupportsParallelCalls() = false, want true")
	}
}

func TestVertexEmbeddingModel_DoEmbedMany(t *testing.T) {
	t.Parallel()

	t.Run("empty inputs", func(t *testing.T) {
		p, err := New(Config{
			Project:     "test-project",
			Location:    "us-central1",
			AccessToken: "test-token",
			BaseURL:     "https://example.test",
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		m := NewEmbeddingModel(p, "text-embedding-005")

		res, err := m.DoEmbedMany(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("DoEmbedMany() error = %v", err)
		}
		if len(res.Embeddings) != 0 {
			t.Fatalf("expected empty embeddings, got %d", len(res.Embeddings))
		}
	})

	t.Run("too many inputs", func(t *testing.T) {
		p, err := New(Config{
			Project:     "test-project",
			Location:    "us-central1",
			AccessToken: "test-token",
			BaseURL:     "https://example.test",
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		m := NewEmbeddingModel(p, "text-embedding-005")

		inputs := make([]string, m.MaxEmbeddingsPerCall()+1)
		_, err = m.DoEmbedMany(context.Background(), inputs, nil)
		if err == nil || !strings.Contains(err.Error(), "too many embedding values") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with provider options and headers", func(t *testing.T) {
		var gotTaskType string
		var gotTitle string
		var gotDim float64
		var gotAutoTruncate bool
		var gotHeader string
		var gotPath string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeader = r.Header.Get("X-Req")
			gotPath = r.URL.Path
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)

			instances := body["instances"].([]interface{})
			first := instances[0].(map[string]interface{})
			gotTaskType, _ = first["task_type"].(string)
			gotTitle, _ = first["title"].(string)
			params := body["parameters"].(map[string]interface{})
			gotDim, _ = params["outputDimensionality"].(float64)
			gotAutoTruncate, _ = params["autoTruncate"].(bool)

			_, _ = w.Write([]byte(`{"predictions":[{"embeddings":{"values":[1,2],"statistics":{"token_count":2}}},{"embeddings":{"values":[3,4],"statistics":{"token_count":3}}}]}`))
		}))
		defer server.Close()

		p, err := New(Config{
			Project:     "test-project",
			Location:    "us-central1",
			AccessToken: "test-token",
			BaseURL:     server.URL,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		m := NewEmbeddingModel(p, "text-embedding-005")
		res, err := m.DoEmbedMany(context.Background(), []string{"first", "second"}, &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{
				"vertex": map[string]interface{}{
					"taskType":             "RETRIEVAL_DOCUMENT",
					"title":                "DocTitle",
					"outputDimensionality": 128,
					"autoTruncate":         true,
				},
			},
			Headers: map[string]string{"X-Req": "yes"},
		})
		if err != nil {
			t.Fatalf("DoEmbedMany() error = %v", err)
		}
		if gotPath != "/models/text-embedding-005:predict" {
			t.Fatalf("path = %q", gotPath)
		}
		if gotHeader != "yes" {
			t.Fatalf("X-Req = %q, want yes", gotHeader)
		}
		if gotTaskType != "RETRIEVAL_DOCUMENT" || gotTitle != "DocTitle" {
			t.Fatalf("unexpected task/title: %q %q", gotTaskType, gotTitle)
		}
		if gotDim != 128 {
			t.Fatalf("outputDimensionality = %v", gotDim)
		}
		if !gotAutoTruncate {
			t.Fatal("autoTruncate should be true")
		}
		if len(res.Embeddings) != 2 {
			t.Fatalf("embedding count = %d, want 2", len(res.Embeddings))
		}
		if res.Usage.TotalTokens != 5 || res.Usage.InputTokens != 5 {
			t.Fatalf("usage tokens = %+v, want 5", res.Usage)
		}
		if len(res.Responses) != 2 {
			t.Fatalf("responses len = %d, want 2", len(res.Responses))
		}
	})

	t.Run("prediction mismatch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"predictions":[{"embeddings":{"values":[1],"statistics":{"token_count":1}}}]}`))
		}))
		defer server.Close()

		p, err := New(Config{
			Project:     "test-project",
			Location:    "us-central1",
			AccessToken: "test-token",
			BaseURL:     server.URL,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		m := NewEmbeddingModel(p, "text-embedding-005")
		_, err = m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
		if err == nil || !strings.Contains(err.Error(), "returned 1 predictions for 2 inputs") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestVertexEmbeddingModel_DoEmbedSingle(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"predictions":[{"embeddings":{"values":[0.25,0.75],"statistics":{"token_count":4}}}]}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m := NewEmbeddingModel(p, "text-embedding-005")

	res, err := m.DoEmbed(context.Background(), "single", nil)
	if err != nil {
		t.Fatalf("DoEmbed() error = %v", err)
	}
	if len(res.Embedding) != 2 {
		t.Fatalf("embedding len = %d, want 2", len(res.Embedding))
	}
	if res.Usage.TotalTokens != 4 {
		t.Fatalf("usage total tokens = %d, want 4", res.Usage.TotalTokens)
	}
}

func TestVertexEmbeddingOptionsAndHeadersHelpers(t *testing.T) {
	t.Parallel()

	fromGoogle := vertexEmbeddingOptions(&provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"taskType": "RETRIEVAL_QUERY",
				"title":    "x",
			},
		},
	})
	if fromGoogle.TaskType != "RETRIEVAL_QUERY" || fromGoogle.Title != "x" {
		t.Fatalf("unexpected options from google key: %+v", fromGoogle)
	}

	if got := embedOptsHeaders(nil); got != nil {
		t.Fatalf("embedOptsHeaders(nil) = %#v, want nil", got)
	}
}
