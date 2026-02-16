package gateway

import (
	"context"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for AI Gateway
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new AI Gateway embedding model
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
	return "gateway"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings that can be generated in a single call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	// Gateway passes through to underlying models, use a conservative default
	return 100
}

// SupportsParallelCalls returns whether the model supports parallel embedding calls
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed generates an embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	// Build request body
	body := map[string]interface{}{
		"value": input,
	}

	// Add headers
	headers := m.getModelConfigHeaders()

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders()
	AddO11yHeaders(headers, o11y)

	// Make API request
	var result types.EmbeddingResult
	err := m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/embedding-model",
		Body:    body,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleError(err)
	}

	return &result, nil
}

// DoEmbedMany generates embeddings for multiple inputs
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	// Build request body
	body := map[string]interface{}{
		"values": inputs,
	}

	// Add headers
	headers := m.getModelConfigHeaders()

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders()
	AddO11yHeaders(headers, o11y)

	// Make API request
	var result types.EmbeddingsResult
	err := m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/embedding-model",
		Body:    body,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleError(err)
	}

	return &result, nil
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *EmbeddingModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-embedding-model-specification-version": "3",
		"ai-embedding-model-id":                    m.modelID,
	}
}

// handleError converts errors to appropriate provider errors
func (m *EmbeddingModel) handleError(err error) error {
	// Use the same error handling as language model
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleError(err)
}
