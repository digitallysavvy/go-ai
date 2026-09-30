package mistral

import (
	"context"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Mistral AI
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Mistral AI embedding model
func NewEmbeddingModel(provider *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return "mistral"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call.
// Mirrors TS MistralEmbeddingModel.maxEmbeddingsPerCall = 32.
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 32
}

// SupportsParallelCalls returns whether parallel calls are supported.
// Mirrors TS MistralEmbeddingModel.supportsParallelCalls = false.
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return false
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	result, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	r := &types.EmbeddingResult{
		Embedding: result.Embeddings[0],
		Usage:     result.Usage,
	}
	if len(result.Responses) > 0 {
		r.Response = result.Responses[0]
	}
	return r, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	reqBody := map[string]interface{}{
		"input": inputs,
		"model": m.modelID,
		// TS MistralEmbeddingModel.doEmbed always sends this.
		"encoding_format": "float",
	}
	if mistralOpts := extractMistralEmbeddingOptions(opts); mistralOpts != nil {
		if mistralOpts.Metadata != nil {
			reqBody["metadata"] = mistralOpts.Metadata
		}
		if mistralOpts.OutputDimension != nil {
			reqBody["output_dimension"] = *mistralOpts.OutputDimension
		}
		if mistralOpts.OutputDtype != "" {
			reqBody["output_dtype"] = mistralOpts.OutputDtype
		}
	}
	var response mistralEmbedResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/embeddings",
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("mistral", 0, "", err.Error(), err)
	}
	embeddings := make([][]float64, len(response.Data))
	for i, item := range response.Data {
		embeddings[i] = item.Embedding
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(response.Usage.PromptTokens),
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
		Responses: []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: response}},
	}, nil
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

// mistralEmbeddingOptions mirrors mistralEmbeddingModelOptions in
// ai/packages/mistral/src/mistral-embedding-model-options.ts.
type mistralEmbeddingOptions struct {
	// Metadata is additional metadata to attach to the embedding request.
	Metadata map[string]interface{}

	// OutputDimension is the dimension of the output embeddings, when
	// supported by the model.
	OutputDimension *int

	// OutputDtype is the data type of the output embeddings, when supported
	// by the model (e.g. "float", "int8", "uint8", "binary", "ubinary").
	OutputDtype string
}

// extractMistralEmbeddingOptions reads providerOptions.mistral for the
// embedding model. Returns nil when no mistral-keyed options are present.
func extractMistralEmbeddingOptions(opts *provider.EmbedModelOptions) *mistralEmbeddingOptions {
	if opts == nil || opts.ProviderOptions == nil {
		return nil
	}
	raw, ok := opts.ProviderOptions["mistral"]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	result := &mistralEmbeddingOptions{}
	if v, ok := m["metadata"].(map[string]interface{}); ok {
		result.Metadata = v
	}
	if v, ok := m["outputDimension"].(float64); ok {
		iv := int(v)
		result.OutputDimension = &iv
	}
	if v, ok := m["outputDtype"].(string); ok {
		result.OutputDtype = v
	}
	return result
}

type mistralEmbedResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
