package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	return "v4"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return m.provider.Name() + ".embedding"
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

// MaxInputBytesPerCall returns the conservative UTF-8 input byte budget for a
// single embeddings request. ai.EmbedMany uses it to split large requests.
// Mirrors the TS SDK's EXPERIMENTAL_EMBEDDING_MODEL_MAX_INPUT_BYTES_PER_CALL = 300_000.
func (m *EmbeddingModel) MaxInputBytesPerCall() int {
	return 300_000
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	if opts != nil {
		if err := m.validateEmbeddingProviderOptions(opts.ProviderOptions); err != nil {
			return nil, err
		}
	}
	reqBody := map[string]interface{}{
		"model": m.modelID,
		"input": input,
	}
	m.applyDimensions(reqBody, opts)

	var response openAIEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: m.responseValidator(),
		Method:           http.MethodPost,
		Path:             "/embeddings",
		Body:             reqBody,
		Headers:          optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return &types.EmbeddingResult{
		Embedding: response.Data[0].Embedding,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(response.Usage.PromptTokens),
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
		Response: types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: response},
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	// TS OpenAICompatibleEmbeddingModel parses provider options before the
	// too-many-values check; OpenAIEmbeddingModel checks the count first.
	validate := func() error {
		if opts == nil {
			return nil
		}
		return m.validateEmbeddingProviderOptions(opts.ProviderOptions)
	}
	if m.provider.config.OpenAICompatible {
		if err := validate(); err != nil {
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
	if !m.provider.config.OpenAICompatible {
		if err := validate(); err != nil {
			return nil, err
		}
	}
	if len(inputs) == 0 {
		return &types.EmbeddingsResult{
			Embeddings: [][]float64{},
			Usage:      types.EmbeddingUsage{},
		}, nil
	}

	reqBody := map[string]interface{}{
		"model":           m.modelID,
		"input":           inputs,
		"encoding_format": "float",
	}
	warnings := m.applyDimensions(reqBody, opts)

	var response openAIEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: m.responseValidator(),
		Method:           http.MethodPost,
		Path:             "/embeddings",
		Body:             reqBody,
		Headers:          optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if len(response.Data) != len(inputs) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(inputs), len(response.Data))
	}

	embeddings := make([][]float64, len(response.Data))
	for i, data := range response.Data {
		if data.Index != i {
			return nil, fmt.Errorf("embedding index mismatch: expected %d, got %d", i, data.Index)
		}
		embeddings[i] = data.Embedding
	}

	// TS response.body is the raw parsed JSON.
	var rawBody interface{} = response
	_ = json.Unmarshal(httpResp.Body, &rawBody)
	result := &types.EmbeddingsResult{
		Embeddings: embeddings,
		Warnings:   warnings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(response.Usage.PromptTokens),
			InputTokens: response.Usage.PromptTokens,
			TotalTokens: response.Usage.TotalTokens,
		},
		Responses: []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: rawBody}},
	}
	if m.provider.config.OpenAICompatible && len(response.ProviderMetadata) > 0 {
		// OpenAICompatibleEmbeddingModel passes the response providerMetadata through.
		result.ProviderMetadata = map[string]interface{}{}
		for k, v := range response.ProviderMetadata {
			result.ProviderMetadata[k] = v
		}
	}
	return result, nil
}

// responseValidator is the TS response schema of the embeddings call:
// openaiTextEmbeddingResponseSchema, or the openai-compatible variant (with
// providerMetadata) for the compatible wrappers.
func (m *EmbeddingModel) responseValidator() func(int, http.Header, []byte) error {
	schema := providerutils.SchemaOpenAIEmbedding
	if m.provider.config.OpenAICompatible {
		schema = providerutils.SchemaCompatEmbedding
	}
	return providerutils.MediaResponseValidator(m.Provider(), schema)
}

// handleError converts various errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
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
	ProviderMetadata map[string]map[string]interface{} `json:"providerMetadata"`
}

// applyDimensions sets the request's dimensions and user. Native OpenAI reads
// providerOptions.openai; the openai-compatible wrappers (baseten, deepinfra)
// read openaiCompatible and their own provider-name namespace, like TS
// OpenAICompatibleEmbeddingModel. The provider option wins over the top-level
// dimensions setting (TS #22338).
func (m *EmbeddingModel) applyDimensions(reqBody map[string]interface{}, opts *provider.EmbedModelOptions) []types.Warning {
	if opts == nil {
		return nil
	}
	var dimensionsOption, userOption interface{}
	var warnings []types.Warning
	if m.provider.config.OpenAICompatible {
		name, _, _ := strings.Cut(m.provider.Name(), ".")
		var resolved map[string]interface{}
		resolved, warnings = providerutils.ResolveOpenAICompatibleProviderOptions(name, opts.ProviderOptions)
		dimensionsOption, userOption = resolved["dimensions"], resolved["user"]
	} else {
		dimensionsOption = providerutils.EmbedProviderOption(opts.ProviderOptions, "openai", "dimensions")
		userOption = providerutils.EmbedProviderOption(opts.ProviderOptions, "openai", "user")
	}
	if d, ok := providerutils.ResolveEmbeddingDimensions(dimensionsOption, opts.Dimensions); ok {
		reqBody["dimensions"] = d
	}
	if u, ok := userOption.(string); ok && u != "" {
		reqBody["user"] = u
	}
	return warnings
}
