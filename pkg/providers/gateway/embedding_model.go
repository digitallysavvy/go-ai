package gateway

import (
	"context"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	return "v4"
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
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	body := map[string]interface{}{
		"value": input,
	}

	headers := m.getModelConfigHeaders()
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)
	if opts != nil {
		for k, v := range opts.Headers {
			headers[k] = v
		}
	}

	var result types.EmbeddingResult
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/embedding-model",
		Body:    body,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}
	result.Response = types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(httpResp.Headers)}

	return &result, nil
}

// DoEmbedMany generates embeddings for multiple inputs
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	body := map[string]interface{}{
		"values": inputs,
	}

	headers := m.getModelConfigHeaders()
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)
	if opts != nil {
		for k, v := range opts.Headers {
			headers[k] = v
		}
	}

	var result types.EmbeddingsResult
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/embedding-model",
		Body:    body,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}
	result.Responses = []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(httpResp.Headers)}}

	return &result, nil
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *EmbeddingModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-embedding-model-specification-version": "4",
		"ai-embedding-model-id":                    m.modelID,
	}
}

// handleError converts errors to appropriate provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return m.handleErrorWithContext(context.Background(), err)
}

func (m *EmbeddingModel) handleErrorWithContext(ctx context.Context, err error) error {
	// Use the same error handling as language model
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleErrorWithContext(ctx, err)
}
