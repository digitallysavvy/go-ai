package bedrock

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Owner ruling 2026-11-02 (option b). TS amazon-bedrock-embedding-model.ts
// always returns `usage: { tokens }`: Titan inputTextTokenCount, Nova
// inputTokenCount ?? 0, Cohere Number(x-amzn-bedrock-input-token-count)
// (NaN when the header is missing, which telemetry drops). DoEmbedMany sums
// the batches as TS embedMany does, so a NaN batch leaves the total
// unreported (the integral Tokens keeps its v0.5 value).
func TestR80carryEmbeddingUsageReported(t *testing.T) {
	cases := []struct {
		name             string
		model            string
		body             string
		header           string
		wantReported     bool
		wantNaN          bool
		wantManyReported bool
	}{
		{"titan count 0", "amazon.titan-embed-text-v2:0", `{"embedding":[1],"inputTextTokenCount":0}`, "", true, false, true},
		{"nova count absent", "amazon.nova-2-multimodal-embeddings-v1:0", `{"embeddings":[{"embeddingType":"TEXT","embedding":[1]}]}`, "", true, false, true},
		{"cohere header 0", "cohere.embed-v4:0", `{"embeddings":{"float":[[1]]}}`, "0", true, false, true},
		{"cohere header absent", "cohere.embed-v4:0", `{"embeddings":{"float":[[1]]}}`, "", true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("x-amzn-bedrock-input-token-count", tc.header)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b", BaseURL: srv.URL})
			m := NewEmbeddingModel(p, tc.model)
			one, err := m.DoEmbed(context.Background(), "hi", nil)
			if err != nil {
				t.Fatal(err)
			}
			if one.Usage.Reported != tc.wantReported || math.IsNaN(one.Usage.Tokens) != tc.wantNaN {
				t.Fatalf("DoEmbed usage = %+v, want Reported=%v NaN=%v", one.Usage, tc.wantReported, tc.wantNaN)
			}
			many, err := m.DoEmbedMany(context.Background(), []string{"hi"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if many.Usage.Reported != tc.wantManyReported || many.Usage.Tokens != 0 {
				t.Fatalf("DoEmbedMany usage = %+v, want Reported=%v Tokens=0", many.Usage, tc.wantManyReported)
			}
		})
	}
}
