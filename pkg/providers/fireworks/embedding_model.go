package fireworks

import (
	"context"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Fireworks AI
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Fireworks AI embedding model
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
	return "fireworks.embedding"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Fireworks AI supports 2048 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 2048
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	result, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
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
	// TS OpenAICompatibleEmbeddingModel parses provider options before the
	// too-many-values check.
	if opts != nil {
		if err := providerutils.ValidateOpenAICompatibleEmbeddingOptions(opts.ProviderOptions, "fireworks"); err != nil {
			return nil, err
		}
	}
	if len(inputs) > m.MaxEmbeddingsPerCall() {
		return nil, &providererrors.TooManyEmbeddingValuesForCallError{
			Provider:             m.Provider(),
			ModelID:              m.modelID,
			MaxEmbeddingsPerCall: m.MaxEmbeddingsPerCall(),
			Values:               inputs,
		}
	}
	reqBody := map[string]interface{}{
		"input": inputs,
		"model": m.modelID,
	}
	applyEmbeddingDimensions(reqBody, opts)
	var response fireworksEmbedResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaCompatEmbedding),
		Method:           http.MethodPost,
		Path:             "/v1/embeddings",
		Body:             reqBody,
		Headers:          optsEmbedHeaders(opts),
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("fireworks", 0, "", err.Error(), err)
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
		Responses: []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: providerutils.RawJSONBody(httpResp.Body)}},
	}, nil
}

// optsEmbedHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsEmbedHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

type fireworksEmbedResponse struct {
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

// applyEmbeddingDimensions sets the request's dimensions: the provider option
// wins over the top-level dimensions setting (TS #22338).
func applyEmbeddingDimensions(reqBody map[string]interface{}, opts *provider.EmbedModelOptions) {
	if opts == nil {
		return
	}
	resolved, _ := providerutils.ResolveOpenAICompatibleProviderOptions("fireworks", opts.ProviderOptions)
	if d, ok := providerutils.ResolveEmbeddingDimensions(resolved["dimensions"], opts.Dimensions); ok {
		reqBody["dimensions"] = d
	}
	// providerOptions.<provider>.user (TS openaiCompatibleEmbeddingModelOptions.user).
	if u, ok := resolved["user"].(string); ok && u != "" {
		reqBody["user"] = u
	}
}
