package together

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Owner ruling 2026-11-02 (option b): EmbeddingUsage.Reported is set only
// when the wire response carried a usage object (TS openai-compatible-embedding-model.ts `response.usage ? { tokens: response.usage.prompt_tokens } : undefined`).
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
			m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "embed")
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

// TS embed() rejects a response without embeddings with
// InvalidResponseDataError("No embedding generated.", data: []).
func TestR80carryDoEmbedNoEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "embed")
	_, err := m.DoEmbed(context.Background(), "a", nil)
	var invalid *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalid) || err.Error() != "No embedding generated." {
		t.Fatalf("got %v, want InvalidResponseDataError", err)
	}
}
