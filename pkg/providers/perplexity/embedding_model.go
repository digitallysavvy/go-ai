package perplexity

import (
	"context"
	"encoding/base64"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// PerplexityEmbeddingModelID identifies a Perplexity embedding model.
// See https://docs.perplexity.ai/docs/embeddings/quickstart.
type PerplexityEmbeddingModelID string

const (
	ModelEmbedV1_0_6B PerplexityEmbeddingModelID = "pplx-embed-v1-0.6b"
	ModelEmbedV1_4B   PerplexityEmbeddingModelID = "pplx-embed-v1-4b"
)

// PerplexityEmbeddingModelOptions mirrors the TS SDK's
// perplexityEmbeddingModelOptions schema.
type PerplexityEmbeddingModelOptions struct {
	// Dimensions is the number of dimensions the resulting output embeddings
	// should have (Matryoshka truncation). Ranges from 128 up to the model's
	// full size (1024 for 0.6b models, 2560 for 4b models).
	Dimensions *int `json:"dimensions,omitempty"`

	// EncodingFormat is the quantized encoding format returned by the API.
	// Perplexity does not return floating point embeddings; values are
	// decoded to numbers:
	//   - "base64_int8" (default): signed int8 values, cosine similarity.
	//   - "base64_binary": packed bits per byte (0-255), Hamming distance.
	EncodingFormat string `json:"encodingFormat,omitempty"`
}

// EmbeddingModel implements the provider.EmbeddingModel interface for Perplexity.
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Perplexity embedding model.
func NewEmbeddingModel(p *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name.
func (m *EmbeddingModel) Provider() string {
	return "perplexity"
}

// ModelID returns the model ID.
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call.
// https://docs.perplexity.ai/docs/embeddings/standard-embeddings
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 512
}

// SupportsParallelCalls returns whether parallel calls are supported.
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input.
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	result, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	r := &types.EmbeddingResult{
		Embedding:        result.Embeddings[0],
		Usage:            result.Usage,
		Warnings:         result.Warnings,
		ProviderMetadata: result.ProviderMetadata,
	}
	if len(result.Responses) > 0 {
		r.Response = result.Responses[0]
	}
	return r, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch.
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) > m.MaxEmbeddingsPerCall() {
		return nil, &providererrors.TooManyEmbeddingValuesForCallError{
			Provider:             m.Provider(),
			ModelID:              m.modelID,
			MaxEmbeddingsPerCall: m.MaxEmbeddingsPerCall(),
			Values:               inputs,
		}
	}

	perplexityOpts := extractEmbeddingOptions(opts)
	encodingFormat := perplexityOpts.EncodingFormat
	if encodingFormat == "" {
		// Perplexity only returns quantized (base64-encoded) embeddings.
		encodingFormat = "base64_int8"
	}

	reqBody := map[string]interface{}{
		"model":           m.modelID,
		"input":           inputs,
		"encoding_format": encodingFormat,
	}
	if perplexityOpts.Dimensions != nil {
		reqBody["dimensions"] = *perplexityOpts.Dimensions
	}

	var response perplexityEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/embeddings",
		Body:    reqBody,
		Headers: embedOptsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("perplexity", 0, "", err.Error(), err)
	}

	embeddings := make([][]float64, len(response.Data))
	for i, item := range response.Data {
		decoded, decodeErr := decodePerplexityEmbedding(item.Embedding, encodingFormat)
		if decodeErr != nil {
			return nil, decodeErr
		}
		embeddings[i] = decoded
	}

	result := &types.EmbeddingsResult{
		Embeddings: embeddings,
		Responses:  []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: response}},
	}
	if response.Usage != nil {
		result.Usage = types.EmbeddingUsage{
			Tokens:      float64(response.Usage.PromptTokens),
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.PromptTokens,
		}
		if response.Usage.Cost != nil {
			var inputCost, totalCost interface{}
			var currency interface{}
			if response.Usage.Cost.InputCost != nil {
				inputCost = *response.Usage.Cost.InputCost
			}
			if response.Usage.Cost.TotalCost != nil {
				totalCost = *response.Usage.Cost.TotalCost
			}
			if response.Usage.Cost.Currency != nil {
				currency = *response.Usage.Cost.Currency
			}
			result.ProviderMetadata = map[string]interface{}{
				"perplexity": map[string]interface{}{
					"cost": map[string]interface{}{
						"inputCost": inputCost,
						"totalCost": totalCost,
						"currency":  currency,
					},
				},
			}
		}
	}

	return result, nil
}

func extractEmbeddingOptions(opts *provider.EmbedModelOptions) PerplexityEmbeddingModelOptions {
	if opts == nil || opts.ProviderOptions == nil {
		return PerplexityEmbeddingModelOptions{}
	}
	raw, ok := opts.ProviderOptions["perplexity"]
	if !ok {
		return PerplexityEmbeddingModelOptions{}
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return PerplexityEmbeddingModelOptions{}
	}
	result := PerplexityEmbeddingModelOptions{}
	if v, ok := m["dimensions"]; ok {
		switch n := v.(type) {
		case int:
			result.Dimensions = &n
		case float64:
			iv := int(n)
			result.Dimensions = &iv
		}
	}
	if v, ok := m["encodingFormat"].(string); ok {
		result.EncodingFormat = v
	}
	return result
}

// embedOptsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func embedOptsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

// decodePerplexityEmbedding decodes a base64-encoded Perplexity embedding into
// a numeric vector. For "base64_int8" the bytes are reinterpreted as signed
// int8 values; for "base64_binary" the raw unsigned bytes (packed bits) are
// returned.
func decodePerplexityEmbedding(encoded string, encodingFormat string) ([]float64, error) {
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	result := make([]float64, len(bytes))
	if encodingFormat == "base64_binary" {
		for i, b := range bytes {
			result[i] = float64(b)
		}
		return result, nil
	}
	// base64_int8: reinterpret raw bytes as signed int8 values.
	for i, b := range bytes {
		result[i] = float64(int8(b))
	}
	return result, nil
}

// perplexityEmbeddingResponse is the minimal wire shape needed from the
// Perplexity embeddings endpoint response.
type perplexityEmbeddingResponse struct {
	Data []struct {
		Embedding string `json:"embedding"`
	} `json:"data"`
	Usage *perplexityEmbeddingUsage `json:"usage"`
}

type perplexityEmbeddingUsage struct {
	PromptTokens int                      `json:"prompt_tokens"`
	Cost         *perplexityEmbeddingCost `json:"cost,omitempty"`
}

type perplexityEmbeddingCost struct {
	InputCost *float64 `json:"input_cost,omitempty"`
	TotalCost *float64 `json:"total_cost,omitempty"`
	Currency  *string  `json:"currency,omitempty"`
}
