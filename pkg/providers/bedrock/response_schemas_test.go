package bedrock

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createJsonResponseHandler: the failure is an APICallError "Invalid JSON
// response" with the response status, headers and body and a
// TypeValidationError (or the JSON parse error) as the cause.
func TestBedrockInvalidJSONResponseCarriesStatusHeadersAndBody(t *testing.T) {
	header := http.Header{"X-Amzn-Requestid": {"req-1"}}
	check := func(t *testing.T, err error, body string) {
		t.Helper()
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != "Invalid JSON response" {
			t.Fatalf("err = %#v, want Invalid JSON response", err)
		}
		if perr.StatusCode != 200 || perr.ResponseBody != body || len(perr.ResponseHeaders) == 0 || perr.Cause == nil {
			t.Fatalf("status=%d body=%q headers=%v cause=%v", perr.StatusCode, perr.ResponseBody, perr.ResponseHeaders, perr.Cause)
		}
	}
	for _, body := range []string{`{"images":"abc"}`, `not json`} {
		_, err := NewImageModel(embeddingTestProvider(body, header), "amazon.nova-canvas-v1:0").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
		check(t, err, body)
		_, err = NewEmbeddingModel(embeddingTestProvider(body, header), "amazon.titan-embed-text-v2:0").DoEmbed(context.Background(), "hi", nil)
		check(t, err, body)
		_, err = NewRerankingModel(embeddingTestProvider(body, header), "amazon.rerank-v1:0").DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
		check(t, err, body)
		lm := NewLanguageModel(embeddingTestProvider(body, header), "anthropic.claude-3-haiku-20240307-v1:0")
		_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		check(t, err, body)
	}
}

// Embedding bodies are a union of the Titan, Nova and Cohere shapes; unknown
// keys pass and an optional token count may be absent.
func TestBedrockEmbeddingUnionSchema(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		bad  bool
	}{
		"titan":              {`{"embedding":[1],"inputTextTokenCount":2,"x":1}`, false},
		"titan no count":     {`{"embedding":[1]}`, true},
		"nova no count":      {`{"embeddings":[{"embeddingType":"TEXT","embedding":[1]}]}`, false},
		"cohere v3":          {`{"embeddings":[[1,2]]}`, false},
		"cohere v4":          {`{"embeddings":{"float":[[1,2]]}}`, false},
		"cohere v4 wrong":    {`{"embeddings":{"float":["a"]}}`, true},
		"nova token string":  {`{"embeddings":[{"embeddingType":"T","embedding":[1]}],"inputTokenCount":"1"}`, true},
		"image null images":  {`{"images":null}`, true},
		"image extra fields": {`{"images":["a"],"result":{"x":1},"progress":2}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			node := zbEmbedding
			if len(name) > 5 && name[:5] == "image" {
				node = zbImage
			}
			err := bedrockValidator(node)(200, http.Header{}, []byte(tc.body))
			if (err != nil) != tc.bad {
				t.Fatalf("err = %v, bad = %v", err, tc.bad)
			}
		})
	}
}
