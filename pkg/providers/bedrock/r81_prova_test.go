package bedrock

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r81provaEmbeddingModel(t *testing.T, modelID string, handler http.HandlerFunc) *EmbeddingModel {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b", BaseURL: srv.URL})
	return NewEmbeddingModel(p, modelID)
}

func r81provaJSON(body string, headers map[string][]string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for k, vs := range headers {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// TS doEmbed maps an empty Nova / Cohere embeddings list to `embeddings: []`
// and embed() rejects it with InvalidResponseDataError "No embedding
// generated."; Go returned the plain error "no embeddings in response".
func TestR81provaBedrockDoEmbedNoEmbeddingsInvalidResponse(t *testing.T) {
	cases := []struct{ name, model, body string }{
		{"nova empty", "amazon.nova-2-multimodal-embeddings-v1:0", `{"embeddings":[],"inputTokenCount":1}`},
		{"cohere v3 empty", "cohere.embed-english-v3", `{"embeddings":[]}`},
		{"cohere v4 empty", "cohere.embed-v4:0", `{"embeddings":{"float":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := r81provaEmbeddingModel(t, tc.model, r81provaJSON(tc.body, nil))
			_, err := m.DoEmbed(context.Background(), "hi", nil)
			var invalid *providererrors.InvalidResponseDataError
			if !errors.As(err, &invalid) || invalid.Message != "No embedding generated." {
				t.Fatalf("err = %T %v, want InvalidResponseDataError \"No embedding generated.\"", err, err)
			}
		})
	}
}

// TS returns the embeddings as sent and embedMany compares the count ("Expected
// N embeddings, but received M."); Go DoEmbedMany failed with its own
// "bedrock cohere embedding batch returned ..." error (Cohere) or the
// single-value error (Titan/Nova).
func TestR81provaBedrockDoEmbedManyKeepsEmbeddingsAsSent(t *testing.T) {
	m := r81provaEmbeddingModel(t, "cohere.embed-v4:0", r81provaJSON(`{"embeddings":{"float":[[1]]}}`, map[string][]string{"x-amzn-bedrock-input-token-count": {"4"}}))
	res, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany: %v", err)
	}
	if len(res.Embeddings) != 1 || res.Usage.Tokens != 4 || len(res.Responses) != 1 {
		t.Fatalf("result = %+v", res)
	}

	nova := r81provaEmbeddingModel(t, "amazon.nova-2-multimodal-embeddings-v1:0", r81provaJSON(`{"embeddings":[],"inputTokenCount":2}`, nil))
	res, err = nova.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err != nil {
		t.Fatalf("Nova DoEmbedMany: %v", err)
	}
	if len(res.Embeddings) != 0 || res.Usage.Tokens != 2 {
		t.Fatalf("Nova result = %+v", res)
	}
}

// TS token count: Titan inputTextTokenCount; Nova inputTokenCount when the
// parsed body has it (zod leaves an absent optional key out), else
// Number(x-amzn-bedrock-input-token-count), which is NaN for a missing header,
// 0 for an empty one and keeps fractions.
func TestR81provaBedrockEmbeddingTokenCount(t *testing.T) {
	const nova = "amazon.nova-2-multimodal-embeddings-v1:0"
	novaBody := `{"embeddings":[{"embeddingType":"TEXT","embedding":[1]}]}`
	cases := []struct {
		name, model, body string
		header            []string
		want              float64 // NaN for NaN
	}{
		{"nova body count", nova, `{"embeddings":[{"embeddingType":"TEXT","embedding":[1]}],"inputTokenCount":3}`, []string{"9"}, 3},
		{"nova header count", nova, novaBody, []string{"7"}, 7},
		{"nova no count", nova, novaBody, nil, math.NaN()},
		{"nova empty embeddingType", nova, `{"embeddings":[{"embeddingType":"","embedding":[1]}],"inputTokenCount":5}`, nil, 5},
		{"cohere empty header", "cohere.embed-v4:0", `{"embeddings":{"float":[[1]]}}`, []string{""}, 0},
		{"cohere fractional header", "cohere.embed-v4:0", `{"embeddings":{"float":[[1]]}}`, []string{" 12.5 "}, 12.5},
		{"cohere hex header", "cohere.embed-english-v3", `{"embeddings":[[1]]}`, []string{"0x10"}, 16},
		{"cohere text header", "cohere.embed-v4:0", `{"embeddings":{"float":[[1]]}}`, []string{"many"}, math.NaN()},
		{"titan fractional", "amazon.titan-embed-text-v2:0", `{"embedding":[1],"inputTextTokenCount":2.5}`, []string{"9"}, 2.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h map[string][]string
			if tc.header != nil {
				h = map[string][]string{"x-amzn-bedrock-input-token-count": tc.header}
			}
			m := r81provaEmbeddingModel(t, tc.model, r81provaJSON(tc.body, h))
			res, err := m.DoEmbed(context.Background(), "hi", nil)
			if err != nil {
				t.Fatalf("DoEmbed: %v", err)
			}
			if len(res.Embedding) != 1 || res.Embedding[0] != 1 {
				t.Fatalf("embedding = %v", res.Embedding)
			}
			got := res.Usage.Tokens
			if math.IsNaN(tc.want) != math.IsNaN(got) || (!math.IsNaN(tc.want) && got != tc.want) || !res.Usage.Reported {
				t.Fatalf("usage = %+v, want Tokens %v", res.Usage, tc.want)
			}
			if !math.IsNaN(tc.want) && res.Usage.InputTokens != int(tc.want) {
				t.Fatalf("InputTokens = %d, want %d", res.Usage.InputTokens, int(tc.want))
			}
		})
	}
}

// TS returns the image strings and core decodes them with
// convertBase64ToUint8Array; Image/Images carry the decoded bytes, which Go
// built with base64.StdEncoding (an unpadded payload kept the wire text).
func TestR81provaBedrockImageForgivingBase64(t *testing.T) {
	srv := httptest.NewServer(r81provaJSON(`{"images":["-_8"]}`, nil))
	defer srv.Close()
	m := NewImageModel(New(Config{Region: "us-east-1", AWSAccessKeyID: "a", AWSSecretAccessKey: "b", BaseURL: srv.URL}), "amazon.nova-canvas-v1:0")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if string(res.Image) != "\xfb\xff" || res.Base64Images[0] != "-_8" {
		t.Fatalf("image = %x base64 = %v", res.Image, res.Base64Images)
	}
}

// A bare image type on a base64 string payload is sniffed from its bytes;
// TS detectMediaType decodes the string with convertBase64ToUint8Array, which
// ignores ASCII whitespace such as a space (types.DecodeFileDataString
// rejected it).
func TestR81provaBedrockDataStringSniffIgnoresWhitespace(t *testing.T) {
	payload := fileContentPayload(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "iVBO Rw=="}})
	got, err := resolveBedrockDataMediaType("image", payload)
	if err != nil || got != "image/png" || payload.bytes != "iVBO Rw==" {
		t.Fatalf("media type = %q, %v; bytes %q", got, err, payload.bytes)
	}
}
