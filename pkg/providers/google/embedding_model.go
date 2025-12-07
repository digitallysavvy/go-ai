package google

import (
	"context"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Google
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Google embedding model
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
	return "google"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Google supports 100 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 100
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	// Build request body
	reqBody := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", m.modelID),
		"content": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": input},
			},
		},
	}

	// Build path with API key
	path := fmt.Sprintf("/v1beta/models/%s:embedContent?key=%s", m.modelID, m.provider.APIKey())

	// Make API request
	var response googleEmbeddingResponse
	err := m.provider.client.PostJSON(ctx, path, reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Validate response
	if response.Embedding == nil || len(response.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	// Convert response to EmbeddingResult
	return &types.EmbeddingResult{
		Embedding: response.Embedding.Values,
		Usage: types.EmbeddingUsage{
			// Google doesn't return token counts for embeddings
			InputTokens: 0,
			TotalTokens: 0,
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

	// Google doesn't have a native batch API, so we'll call individually
	// TODO: Optimize with concurrent requests
	embeddings := make([][]float64, len(inputs))
	for i, input := range inputs {
		result, err := m.DoEmbed(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to embed input %d: %w", i, err)
		}
		embeddings[i] = result.Embedding
	}

	// Convert response to EmbeddingsResult
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: 0,
			TotalTokens: 0,
		},
	}, nil
}

// handleError converts various errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return providererrors.NewProviderError("google", 0, "", err.Error(), err)
}

// googleEmbeddingResponse represents the Google embeddings API response
type googleEmbeddingResponse struct {
	Embedding *struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}
