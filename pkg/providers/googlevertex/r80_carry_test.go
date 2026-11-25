package googlevertex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Owner ruling 2026-11-02 (option b). TS google-vertex-embedding-model.ts:
// :predict always returns `usage: { tokens: sum(statistics.token_count) }`;
// :embedContent returns `usageMetadata?.promptTokenCount == null ? undefined
// : { tokens }`.
func TestR80carryEmbeddingUsageReported(t *testing.T) {
	cases := []struct {
		name         string
		model        string
		body         string
		wantReported bool
	}{
		{"predict count 0", "text-embedding-004", `{"predictions":[{"embeddings":{"values":[1],"statistics":{"token_count":0}}}]}`, true},
		{"embedContent count 0", "gemini-embedding-2", `{"embedding":{"values":[1]},"usageMetadata":{"promptTokenCount":0}}`, true},
		{"embedContent count null", "gemini-embedding-2", `{"embedding":{"values":[1]},"usageMetadata":{"promptTokenCount":null}}`, false},
		{"embedContent empty metadata", "gemini-embedding-2", `{"embedding":{"values":[1]},"usageMetadata":{}}`, false},
		{"embedContent no metadata", "gemini-embedding-2", `{"embedding":{"values":[1]}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			res, err := NewEmbeddingModel(p, tc.model).DoEmbed(context.Background(), "a", nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Usage.Reported != tc.wantReported || res.Usage.Tokens != 0 {
				t.Fatalf("usage = %+v, want Reported=%v Tokens=0", res.Usage, tc.wantReported)
			}
		})
	}
}
