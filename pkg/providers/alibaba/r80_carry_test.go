package alibaba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Owner ruling 2026-11-02 (option b): EmbeddingUsage.Reported is set only
// when the wire response carried a usage object (TS alibaba-embedding-model.ts `response.usage ? { tokens: response.usage.total_tokens } : undefined`).
func TestR80carryEmbeddingUsageReported(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantReported bool
		wantTokens   float64
	}{
		{"usage 0", `{"output":{"embeddings":[{"embedding":[1,2],"text_index":0}]},"usage":{"total_tokens":0}}`, true, 0},
		{"usage null", `{"output":{"embeddings":[{"embedding":[1,2],"text_index":0}]},"usage":null}`, false, 0},
		{"usage absent", `{"output":{"embeddings":[{"embedding":[1,2],"text_index":0}]}}`, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			m := NewEmbeddingModel(New(Config{APIKey: "k", EmbeddingBaseURL: srv.URL}), AlibabaEmbeddingTextV4)
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
