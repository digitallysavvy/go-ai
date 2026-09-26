package googlevertex

import (
	"context"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Google Vertex AI.
// It uses the Vertex AI text embeddings prediction API.
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Google Vertex AI embedding model.
func NewEmbeddingModel(p *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{
		provider: p,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version.
func (m *EmbeddingModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *EmbeddingModel) Provider() string { return "google-vertex" }

// ModelID returns the model ID.
func (m *EmbeddingModel) ModelID() string { return m.modelID }

// MaxEmbeddingsPerCall returns the maximum number of embeddings per batch call.
// gemini-embedding-2 / gemini-embedding-2-preview only support the
// :embedContent endpoint, which accepts one value per call; every other
// Vertex embedding model supports up to 250 per :predict call (TS
// google-vertex-embedding-model.ts maxEmbeddingsPerCall).
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	if usesEmbedContentEndpoint(m.modelID) {
		return 1
	}
	return 250
}

// usesEmbedContentEndpoint reports whether modelID must use the
// :embedContent endpoint instead of the batch :predict endpoint.
// https://github.com/vercel/ai/issues/15853
func usesEmbedContentEndpoint(modelID string) bool {
	return modelID == EmbeddingModelGeminiEmbedding2 || modelID == EmbeddingModelGeminiEmbedding2Preview
}

// SupportsParallelCalls returns whether parallel calls are supported.
func (m *EmbeddingModel) SupportsParallelCalls() bool { return true }

// VertexEmbeddingProviderOptions holds Vertex-specific embedding options.
type VertexEmbeddingProviderOptions struct {
	// OutputDimensionality is the optional reduced dimension for the output embedding.
	OutputDimensionality *int
	// TaskType specifies the task type for generating embeddings.
	// Valid values: SEMANTIC_SIMILARITY, CLASSIFICATION, CLUSTERING,
	// RETRIEVAL_DOCUMENT, RETRIEVAL_QUERY, QUESTION_ANSWERING,
	// FACT_VERIFICATION, CODE_RETRIEVAL_QUERY.
	TaskType string
	// Title is the title of the document being embedded.
	// Only valid when TaskType is RETRIEVAL_DOCUMENT.
	Title string
	// AutoTruncate controls whether input text is truncated when too long.
	// Defaults to true. When false, an error is returned for oversized input.
	AutoTruncate *bool
}

// DoEmbed performs embedding for a single text input.
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	result, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	resp := types.EmbeddingResponse{}
	if len(result.Responses) > 0 {
		resp = result.Responses[0]
	}
	return &types.EmbeddingResult{
		Embedding: result.Embeddings[0],
		Usage:     result.Usage,
		Response:  resp,
	}, nil
}

// DoEmbedMany performs embedding for multiple text inputs in a single batch call.
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) == 0 {
		return &types.EmbeddingsResult{Embeddings: [][]float64{}, Usage: types.EmbeddingUsage{}}, nil
	}
	if len(inputs) > m.MaxEmbeddingsPerCall() {
		return nil, fmt.Errorf("too many embedding values: %d exceeds max %d per call", len(inputs), m.MaxEmbeddingsPerCall())
	}

	vopts := vertexEmbeddingOptions(opts)

	if usesEmbedContentEndpoint(m.modelID) {
		return m.doEmbedContent(ctx, inputs[0], vopts, opts)
	}

	instances := make([]map[string]interface{}, len(inputs))
	for i, v := range inputs {
		inst := map[string]interface{}{"content": v}
		if vopts.TaskType != "" {
			inst["task_type"] = vopts.TaskType
		}
		if vopts.Title != "" {
			inst["title"] = vopts.Title
		}
		instances[i] = inst
	}

	params := map[string]interface{}{}
	if vopts.OutputDimensionality != nil {
		params["outputDimensionality"] = *vopts.OutputDimensionality
	}
	if vopts.AutoTruncate != nil {
		params["autoTruncate"] = *vopts.AutoTruncate
	}

	body := map[string]interface{}{"instances": instances}
	if len(params) > 0 {
		body["parameters"] = params
	}

	path := fmt.Sprintf("/models/%s:predict", m.modelID)

	var response vertexEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    body,
		Headers: embedOptsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, fmt.Errorf("vertex embedding request failed: %w", err)
	}

	if len(response.Predictions) != len(inputs) {
		return nil, fmt.Errorf("vertex returned %d predictions for %d inputs", len(response.Predictions), len(inputs))
	}

	embeddings := make([][]float64, len(inputs))
	var totalTokens int
	for i, pred := range response.Predictions {
		embeddings[i] = pred.Embeddings.Values
		totalTokens += pred.Embeddings.Statistics.TokenCount
	}

	respEntry := types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: response}
	responses := make([]types.EmbeddingResponse, len(inputs))
	for i := range inputs {
		responses[i] = respEntry
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(totalTokens),
			InputTokens: totalTokens,
			TotalTokens: totalTokens,
		},
		Responses: responses,
	}, nil
}

// doEmbedContent embeds a single value via the :embedContent endpoint, used
// for gemini-embedding-2 / gemini-embedding-2-preview which do not support
// the batch :predict endpoint (TS google-vertex-embedding-model.ts).
func (m *EmbeddingModel) doEmbedContent(ctx context.Context, value string, vopts VertexEmbeddingProviderOptions, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	config := map[string]interface{}{}
	if vopts.OutputDimensionality != nil {
		config["outputDimensionality"] = *vopts.OutputDimensionality
	}
	if vopts.TaskType != "" {
		config["taskType"] = vopts.TaskType
	}
	if vopts.Title != "" {
		config["title"] = vopts.Title
	}
	if vopts.AutoTruncate != nil {
		config["autoTruncate"] = *vopts.AutoTruncate
	}

	body := map[string]interface{}{
		"content": map[string]interface{}{
			"parts": []map[string]interface{}{{"text": value}},
		},
	}
	if len(config) > 0 {
		body["embedContentConfig"] = config
	}

	path := fmt.Sprintf("/models/%s:embedContent", m.modelID)

	var response vertexEmbedContentResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    body,
		Headers: embedOptsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, fmt.Errorf("vertex embedding request failed: %w", err)
	}

	usage := types.EmbeddingUsage{}
	if response.UsageMetadata != nil {
		tokens := float64(response.UsageMetadata.PromptTokenCount)
		usage = types.EmbeddingUsage{
			Tokens:      tokens,
			InputTokens: response.UsageMetadata.PromptTokenCount,
			TotalTokens: response.UsageMetadata.PromptTokenCount,
		}
	}

	return &types.EmbeddingsResult{
		Embeddings: [][]float64{response.Embedding.Values},
		Usage:      usage,
		Responses: []types.EmbeddingResponse{{
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    response,
		}},
	}, nil
}

// vertexEmbedContentResponse is the Vertex AI :embedContent API response,
// used only by gemini-embedding-2 / gemini-embedding-2-preview.
type vertexEmbedContentResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
	UsageMetadata *struct {
		PromptTokenCount int `json:"promptTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

// vertexEmbeddingOptions extracts Vertex embedding provider options from EmbedModelOptions.
// It checks the "vertex" key first, then "google" for cross-provider compatibility.
func vertexEmbeddingOptions(opts *provider.EmbedModelOptions) VertexEmbeddingProviderOptions {
	if opts == nil || opts.ProviderOptions == nil {
		return VertexEmbeddingProviderOptions{}
	}
	for _, key := range []string{"vertex", "google"} {
		if v, ok := opts.ProviderOptions[key]; ok {
			if m, ok := v.(map[string]interface{}); ok {
				result := VertexEmbeddingProviderOptions{}
				if t, ok := m["taskType"].(string); ok {
					result.TaskType = t
				}
				if t, ok := m["title"].(string); ok {
					result.Title = t
				}
				if d, ok := m["outputDimensionality"].(int); ok {
					result.OutputDimensionality = &d
				}
				if a, ok := m["autoTruncate"].(bool); ok {
					result.AutoTruncate = &a
				}
				return result
			}
		}
	}
	return VertexEmbeddingProviderOptions{}
}

// embedOptsHeaders extracts headers from EmbedModelOptions (nil-safe).
func embedOptsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

// vertexEmbeddingResponse is the Vertex AI text embeddings API response.
type vertexEmbeddingResponse struct {
	Predictions []struct {
		Embeddings struct {
			Values     []float64 `json:"values"`
			Statistics struct {
				TokenCount int `json:"token_count"`
			} `json:"statistics"`
		} `json:"embeddings"`
	} `json:"predictions"`
}
