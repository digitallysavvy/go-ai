package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Owner ruling 2026-11-02 (option b): EmbeddingUsage.Reported is set only
// when the wire response carried a usage object (TS openai-embedding-model.ts `response.usage ? { tokens: response.usage.prompt_tokens } : undefined`).
func TestR80carryEmbeddingUsageReported(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantReported bool
		wantTokens   float64
	}{
		{"usage 0", `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,2]}],"model":"m","usage":{"prompt_tokens":0,"total_tokens":0}}`, true, 0},
		{"usage 3", `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,2]}],"model":"m","usage":{"prompt_tokens":3,"total_tokens":3}}`, true, 3},
		{"usage null", `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,2]}],"model":"m","usage":null}`, false, 0},
		{"usage absent", `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,2]}],"model":"m"}`, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "text-embedding-3-small")
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
