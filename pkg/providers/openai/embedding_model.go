package openai

import (
	"context"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for OpenAI
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new OpenAI embedding model
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
	return "openai"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// OpenAI supports up to 2048 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 2048
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	// Build request body
	reqBody := map[string]interface{}{
		"model": m.modelID,
		"input": input,
	}

	// Make API request
	var response openAIEmbeddingResponse
	err := m.provider.client.PostJSON(ctx, "/embeddings", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Validate response
	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	// Convert response to EmbeddingResult
	return &types.EmbeddingResult{
		Embedding: response.Data[0].Embedding,
		Usage: types.EmbeddingUsage{
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	if len(inputs) == 0 {
		return &types.EmbeddingsResult{
			Embeddings: [][]float64{},
			Usage:      types.EmbeddingUsage{},
		}, nil
	}

	// Build request body
	reqBody := map[string]interface{}{
		"model": m.modelID,
		"input": inputs,
	}

	// Make API request
	var response openAIEmbeddingResponse
	err := m.provider.client.PostJSON(ctx, "/embeddings", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Validate response
	if len(response.Data) != len(inputs) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(inputs), len(response.Data))
	}

	// Extract embeddings
	embeddings := make([][]float64, len(response.Data))
	for i, data := range response.Data {
		// Verify index matches expected order
		if data.Index != i {
			return nil, fmt.Errorf("embedding index mismatch: expected %d, got %d", i, data.Index)
		}
		embeddings[i] = data.Embedding
	}

	// Convert response to EmbeddingsResult
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
	}, nil
}

// handleError converts various errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return providererrors.NewProviderError("openai", 0, "", err.Error(), err)
}

// openAIEmbeddingResponse represents the OpenAI embeddings API response
type openAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
