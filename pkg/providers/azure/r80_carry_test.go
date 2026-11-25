package azure

import (
	"context"
	"net/http"
	"testing"
)

// Owner ruling 2026-11-02 (option b): Azure embeddings use the TS
// OpenAIEmbeddingModel, `response.usage ? { tokens: response.usage.prompt_tokens }
// : undefined`, so Reported is set only when the response carried usage.
func TestR80carryEmbeddingUsageReported(t *testing.T) {
	const data = `{"object":"list","data":[{"index":0,"embedding":[1]}]`
	cases := []struct {
		name         string
		body         string
		wantReported bool
	}{
		{"usage 0", data + `,"usage":{"prompt_tokens":0,"total_tokens":0}}`, true},
		{"usage null", data + `,"usage":null}`, false},
		{"usage absent", data + `}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
				return azureJSONResponse(200, tc.body, nil), nil
			})
			m := NewEmbeddingModel(p, "dep")
			res, err := m.DoEmbed(context.Background(), "x", nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Usage.Reported != tc.wantReported || res.Usage.Tokens != 0 {
				t.Fatalf("usage = %+v, want Reported=%v Tokens=0", res.Usage, tc.wantReported)
			}
		})
	}
}
