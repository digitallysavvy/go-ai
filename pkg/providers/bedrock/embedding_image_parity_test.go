package bedrock

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func embeddingTestProvider(body string, header http.Header) *Provider {
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
		Headers: map[string]string{"Content-Type": "application/json"},
		HTTPClient: &http.Client{Transport: bedrockRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
		})},
	})
	return p
}

// TS validates the embedding providerOptions with zod for every model family
// and throws an InvalidArgumentError.
func TestBedrockEmbeddingRejectsInvalidProviderOptions(t *testing.T) {
	p := embeddingTestProvider(`{"embedding":[0.1],"inputTextTokenCount":1}`, http.Header{})
	cases := map[string]map[string]interface{}{
		"dimensions":       {"dimensions": 7},
		"normalize":        {"normalize": "yes"},
		"embeddingPurpose": {"embeddingPurpose": "NOPE"},
		"inputType":        {"inputType": "x"},
		"outputDimension":  {"outputDimension": 5},
	}
	for name, options := range cases {
		_, err := NewEmbeddingModel(p, "amazon.titan-embed-text-v2:0").DoEmbed(context.Background(), "hi", &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"amazonBedrock": options},
		})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions" {
			t.Errorf("%s: err = %v, want InvalidArgumentError(providerOptions)", name, err)
		}
	}
	_, err := NewEmbeddingModel(p, "amazon.titan-embed-text-v2:0").DoEmbed(context.Background(), "hi", &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"dimensions": 512}},
	})
	if err != nil {
		t.Fatalf("valid legacy options: %v", err)
	}
}

// TS validates image providerOptions types too (not only enums).
func TestBedrockImageRejectsWrongTypedProviderOptions(t *testing.T) {
	model := NewImageModel(New(Config{Region: "us-east-1"}), "amazon.nova-canvas-v1:0")
	for name, options := range map[string]map[string]interface{}{
		"negativeText": {"negativeText": 5},
		"cfgScale":     {"cfgScale": "high"},
		"maskPrompt":   {"maskPrompt": true},
	} {
		_, _, err := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "cat", ProviderOptions: map[string]interface{}{"amazonBedrock": options}})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// TS picks the token count from the response shape, not the model family: a
// Cohere-shaped response reports tokens in the header.
func TestBedrockEmbeddingTokenCountFollowsResponseShape(t *testing.T) {
	p := embeddingTestProvider(`{"embeddings":[[0.4,0.5]]}`, http.Header{"X-Amzn-Bedrock-Input-Token-Count": []string{"9"}})
	result, err := NewEmbeddingModel(p, "amazon.titan-embed-text-v2:0").DoEmbed(context.Background(), "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.Tokens != 9 {
		t.Fatalf("tokens = %v, want 9", result.Usage.Tokens)
	}
}

// TS validates the embedding, rerank and image response bodies with zod; a
// mismatch is an "Invalid JSON response" API call error.
func TestBedrockResponsesAreSchemaValidated(t *testing.T) {
	var apiErr *providererrors.ProviderError
	p := embeddingTestProvider(`{"embedding":[0.1]}`, http.Header{})
	_, err := NewEmbeddingModel(p, "amazon.titan-embed-text-v2:0").DoEmbed(context.Background(), "hi", nil)
	if !errors.As(err, &apiErr) || !strings.Contains(err.Error(), "Invalid JSON response") {
		t.Fatalf("embedding err = %v", err)
	}
	p = embeddingTestProvider(`{"results":[{"index":0}]}`, http.Header{})
	_, err = NewRerankingModel(p, "amazon.rerank-v1:0").DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []string{"a"}})
	if !errors.As(err, &apiErr) || !strings.Contains(err.Error(), "Invalid JSON response") {
		t.Fatalf("rerank err = %v", err)
	}
	p = embeddingTestProvider(`{"images":"abc"}`, http.Header{})
	_, err = NewImageModel(p, "amazon.nova-canvas-v1:0").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
	if !errors.As(err, &apiErr) || !strings.Contains(err.Error(), "Invalid JSON response") {
		t.Fatalf("image err = %v", err)
	}
}
