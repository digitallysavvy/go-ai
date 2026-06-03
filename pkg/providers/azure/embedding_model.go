package azure

import (
	"context"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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
	return "azure.embeddings"
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
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	reqBody := map[string]interface{}{
		"input": input,
		"model": m.deploymentID,
	}
	path := m.provider.endpointPath(m.deploymentID, "/embeddings")

	var response azureEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned from Azure OpenAI")
	}

	return &types.EmbeddingResult{
		Embedding: response.Data[0].Embedding,
		Usage: types.EmbeddingUsage{
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
		Response: types.EmbeddingResponse{Headers: map[string][]string(httpResp.Headers)},
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	reqBody := map[string]interface{}{
		"input": inputs,
		"model": m.deploymentID,
	}
	path := m.provider.endpointPath(m.deploymentID, "/embeddings")

	var response azureEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

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
		Responses: []types.EmbeddingResponse{{Headers: map[string][]string(httpResp.Headers)}},
	}, nil
}

// handleError converts errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
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
