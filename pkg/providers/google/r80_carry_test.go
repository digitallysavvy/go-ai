package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Owner ruling 2026-11-02 (option b): EmbeddingUsage.Reported is set only
// when the wire response carried a usage object (TS google-embedding-model.ts always returns `usage: undefined`).
func TestR80carryEmbeddingUsageReported(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantReported bool
		wantTokens   float64
	}{
		{"no usage", `{"embeddings":[{"values":[1,2]}],"embedding":{"values":[1,2]}}`, false, 0},
		{"usage metadata ignored", `{"embeddings":[{"values":[1,2]}],"embedding":{"values":[1,2]},"usageMetadata":{"promptTokenCount":0}}`, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-embedding-001")
			res, err := m.DoEmbedMany(context.Background(), []string{"a"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Usage.Reported != tc.wantReported || res.Usage.Tokens != tc.wantTokens {
				t.Fatalf("usage = %+v, want Reported=%v Tokens=%v", res.Usage, tc.wantReported, tc.wantTokens)
			}
			one, err := m.DoEmbed(context.Background(), "a", nil)
			if err != nil {
				t.Fatal(err)
			}
			if one.Usage.Reported != tc.wantReported {
				t.Fatalf("DoEmbed usage = %+v, want Reported=%v", one.Usage, tc.wantReported)
			}
		})
	}
}

// TS maps `response.embeddings.map(item => item.values)` without checking
// the count or the values (embedMany compares the count with the values).
func TestR80carryDoEmbedManyMapsEmbeddingsAsIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[{"values":[]},{"values":[1]},{"values":[2]}]}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-embedding-001")
	res, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil || len(res.Embeddings) != 3 || res.Embeddings[0] == nil || len(res.Embeddings[0]) != 0 {
		t.Fatalf("got %+v, %v; want the three embeddings as sent", res, err)
	}
}
