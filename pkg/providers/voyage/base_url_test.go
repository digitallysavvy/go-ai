package voyage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS withoutTrailingSlash: a configured base URL ending in "/" must not
// produce "//embeddings".
func TestBaseURLTrailingSlashIsStripped(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1/"}), ModelVoyage3)
	_, _ = m.DoEmbedMany(context.Background(), []string{"a"}, &provider.EmbedModelOptions{})
	if path != "/v1/embeddings" {
		t.Fatalf("request path = %q, want /v1/embeddings", path)
	}
}
