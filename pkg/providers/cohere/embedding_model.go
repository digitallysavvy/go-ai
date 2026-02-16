package cohere

import (
	"context"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Cohere
type EmbeddingModel struct {
	provider *Provider
	modelID  string
	options  EmbeddingOptions
}

// NewEmbeddingModel creates a new Cohere embedding model
func NewEmbeddingModel(provider *Provider, modelID string, options ...EmbeddingOptions) *EmbeddingModel {
	opts := DefaultEmbeddingOptions()
	if len(options) > 0 {
		opts = options[0]
	}
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
		options:  opts,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return "cohere"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Cohere supports 96 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 96
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	result, err := m.DoEmbedMany(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	return &types.EmbeddingResult{
		Embedding: result.Embeddings[0],
		Usage:     result.Usage,
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	// Validate options
	if err := m.options.Validate(); err != nil {
		return nil, err
	}

	reqBody := map[string]interface{}{
		"texts": inputs,
		"model": m.modelID,
	}

	// Add input type if specified
	if m.options.InputType != "" {
		reqBody["input_type"] = string(m.options.InputType)
	} else {
		reqBody["input_type"] = "search_document"
	}

	// Add truncate mode if specified
	if m.options.Truncate != "" {
		reqBody["truncate"] = string(m.options.Truncate)
	}

	// Add output dimension if specified
	if m.options.OutputDimension != nil {
		reqBody["output_dimension"] = int(*m.options.OutputDimension)
	}

	var response cohereEmbedResponse
	err := m.provider.client.PostJSON(ctx, "/v1/embed", reqBody, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
	}
	return &types.EmbeddingsResult{
		Embeddings: response.Embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: response.Meta.BilledUnits.InputTokens,
			TotalTokens: response.Meta.BilledUnits.InputTokens,
		},
	}, nil
}

type cohereEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Meta       struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}
