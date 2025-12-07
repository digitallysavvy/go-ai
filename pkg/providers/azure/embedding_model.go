package azure

import (
	"context"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Azure OpenAI
type EmbeddingModel struct {
	provider     *Provider
	deploymentID string
}

// NewEmbeddingModel creates a new Azure OpenAI embedding model
func NewEmbeddingModel(provider *Provider, deploymentID string) *EmbeddingModel {
	return &EmbeddingModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return "azure-openai"
}

// ModelID returns the deployment ID
func (m *EmbeddingModel) ModelID() string {
	return m.deploymentID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Azure OpenAI supports 2048 embeddings per API call
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
		"input": input,
	}

	// Make API request to Azure-specific endpoint
	path := fmt.Sprintf("/openai/deployments/%s/embeddings?api-version=%s",
		m.deploymentID, m.provider.APIVersion())

	var response azureEmbeddingResponse
	err := m.provider.client.PostJSON(ctx, path, reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response
	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned from Azure OpenAI")
	}

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
	// Build request body
	reqBody := map[string]interface{}{
		"input": inputs,
	}

	// Make API request to Azure-specific endpoint
	path := fmt.Sprintf("/openai/deployments/%s/embeddings?api-version=%s",
		m.deploymentID, m.provider.APIVersion())

	var response azureEmbeddingResponse
	err := m.provider.client.PostJSON(ctx, path, reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response
	embeddings := make([][]float64, len(response.Data))
	for i, data := range response.Data {
		embeddings[i] = data.Embedding
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
	}, nil
}

// handleError converts errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return providererrors.NewProviderError("azure-openai", 0, "", err.Error(), err)
}

// azureEmbeddingResponse represents the Azure OpenAI embeddings API response
type azureEmbeddingResponse struct {
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
