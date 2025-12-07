package together

import (
	"context"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Together AI
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Together AI embedding model
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
	return "together"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Together AI supports 2048 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 2048
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
	reqBody := map[string]interface{}{
		"input": inputs,
		"model": m.modelID,
	}
	var response togetherEmbedResponse
	err := m.provider.client.PostJSON(ctx, "/v1/embeddings", reqBody, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("together", 0, "", err.Error(), err)
	}
	embeddings := make([][]float64, len(response.Data))
	for i, item := range response.Data {
		embeddings[i] = item.Embedding
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
	}, nil
}

type togetherEmbedResponse struct {
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
