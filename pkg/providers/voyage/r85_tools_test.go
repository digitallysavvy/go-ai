package voyage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TS voyageEmbeddingResponseSchema has index and total_tokens as
// z.number(), and sorts with the stable Array.prototype.sort: fractional
// values decode, and equal indexes keep the response order.
func TestR85toolsEmbeddingFractionalIndexAndStableSort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[3],"index":1},{"embedding":[1],"index":0.5},{"embedding":[2],"index":1},{"embedding":[4],"index":1}],"model":"m","usage":{"total_tokens":7.5}}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "voyage-3")
	res, err := m.DoEmbedMany(context.Background(), []string{"a", "b", "c", "d"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]float64{{1}, {3}, {2}, {4}}; !reflect.DeepEqual(res.Embeddings, want) {
		t.Fatalf("embeddings = %v, want %v", res.Embeddings, want)
	}
	if res.Usage.Tokens != 7.5 {
		t.Fatalf("tokens = %v, want 7.5", res.Usage.Tokens)
	}
}
