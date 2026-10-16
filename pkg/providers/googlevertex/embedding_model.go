package googlevertex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
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
	// TS does not check the number of predictions, so a response without any
	// yields an empty embedding rather than an error.
	var embedding []float64
	if len(result.Embeddings) > 0 {
		embedding = result.Embeddings[0]
	}
	return &types.EmbeddingResult{
		Embedding: embedding,
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
		return nil, &providererrors.TooManyEmbeddingValuesForCallError{
			Provider:             m.Provider(),
			ModelID:              m.modelID,
			MaxEmbeddingsPerCall: m.MaxEmbeddingsPerCall(),
			Values:               inputs,
		}
	}

	if err := validateVertexEmbeddingOptions(opts); err != nil {
		return nil, err
	}
	vopts := vertexEmbeddingOptions(opts)
	if vopts.OutputDimensionality == nil && opts != nil && opts.Dimensions != nil {
		// Top-level dimensions (TS #22338); the provider option wins.
		d := *opts.Dimensions
		vopts.OutputDimensionality = &d
	}

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

	// TS always sends the parameters object, empty when no option is set, with
	// outputDimensionality before autoTruncate.
	params := providerutils.OrderedMap{Vals: map[string]interface{}{}}
	if vopts.OutputDimensionality != nil {
		params.Keys = append(params.Keys, "outputDimensionality")
		params.Vals["outputDimensionality"] = *vopts.OutputDimensionality
	}
	if vopts.AutoTruncate != nil {
		params.Keys = append(params.Keys, "autoTruncate")
		params.Vals["autoTruncate"] = *vopts.AutoTruncate
	}

	body := map[string]interface{}{"instances": instances, "parameters": params}

	path := fmt.Sprintf("/models/%s:predict", m.modelID)

	var response vertexEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: vertexValidator(m.Provider(), zVertexTextEmbedding),
		Method:           http.MethodPost,
		Path:             path,
		Body:             body,
		Headers:          embedOptsHeaders(opts),
	}, &response)
	if err != nil {
		// A ProviderError keeps the HTTP status, body and the extracted
		// error.message, so embed/embedMany can retry 429/5xx like TS.
		return nil, vertexEmbeddingError(m.Provider(), err)
	}

	// TS maps every prediction and does not compare their number with the
	// number of inputs.
	embeddings := make([][]float64, len(response.Predictions))
	var totalTokens int
	for i, pred := range response.Predictions {
		embeddings[i] = pred.Embeddings.Values
		totalTokens += pred.Embeddings.Statistics.TokenCount
	}

	respEntry := types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: rawEmbeddingBody(httpResp.Body, response)}
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
	// TS always sends embedContentConfig (empty when no option is set), with
	// the keys in this order.
	config := providerutils.OrderedMap{Vals: map[string]interface{}{}}
	setConfig := func(key string, v interface{}) {
		config.Keys = append(config.Keys, key)
		config.Vals[key] = v
	}
	if vopts.OutputDimensionality != nil {
		setConfig("outputDimensionality", *vopts.OutputDimensionality)
	}
	if vopts.TaskType != "" {
		setConfig("taskType", vopts.TaskType)
	}
	if vopts.Title != "" {
		setConfig("title", vopts.Title)
	}
	if vopts.AutoTruncate != nil {
		setConfig("autoTruncate", *vopts.AutoTruncate)
	}

	body := map[string]interface{}{
		"content": map[string]interface{}{
			"parts": []map[string]interface{}{{"text": value}},
		},
		"embedContentConfig": config,
	}

	path := fmt.Sprintf("/models/%s:embedContent", m.modelID)

	var response vertexEmbedContentResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: vertexValidator(m.Provider(), zVertexEmbedContent),
		Method:           http.MethodPost,
		Path:             path,
		Body:             body,
		Headers:          embedOptsHeaders(opts),
	}, &response)
	if err != nil {
		// A ProviderError keeps the HTTP status, body and the extracted
		// error.message, so embed/embedMany can retry 429/5xx like TS.
		return nil, vertexEmbeddingError(m.Provider(), err)
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
			Body:    rawEmbeddingBody(httpResp.Body, response),
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

// vertexEmbeddingOptions extracts Vertex embedding provider options from
// EmbedModelOptions. It checks "googleVertex" first, then "vertex", then
// "google" for cross-provider compatibility, matching TS
// google-vertex-embedding-model.ts's doEmbed (and the language model's own
// ProviderOptionsKeys order, from which this previously diverged).
func vertexEmbeddingOptions(opts *provider.EmbedModelOptions) VertexEmbeddingProviderOptions {
	if opts == nil || opts.ProviderOptions == nil {
		return VertexEmbeddingProviderOptions{}
	}
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		if v, ok := opts.ProviderOptions[key]; ok {
			if m, ok := v.(map[string]interface{}); ok {
				result := VertexEmbeddingProviderOptions{}
				if t, ok := m["taskType"].(string); ok {
					result.TaskType = t
				}
				if t, ok := m["title"].(string); ok {
					result.Title = t
				}
				// CoerceInt (not a bare `.(int)` assertion) because
				// ProviderOptions commonly arrives via encoding/json.Unmarshal,
				// which decodes JSON numbers as float64, not int.
				if d, ok := providerutils.CoerceInt(m["outputDimensionality"]); ok {
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

// rawEmbeddingBody is the response body as sent (TS response.body =
// rawValue), so fields outside the minimal schema (e.g. metadata) survive; it
// matches the Google Generative AI embedding model's json.RawMessage body.
func rawEmbeddingBody(body []byte, typed interface{}) interface{} {
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	return typed
}

// validateVertexEmbeddingOptions mirrors TS parseProviderOptions with
// googleVertexEmbeddingModelOptions: the first of googleVertex, vertex and
// google that is present is validated, and a wrong type or an unknown task
// type is an InvalidArgumentError.
func validateVertexEmbeddingOptions(opts *provider.EmbedModelOptions) error {
	if opts == nil || opts.ProviderOptions == nil {
		return nil
	}
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		v, ok := opts.ProviderOptions[key]
		if !ok {
			continue
		}
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil
		}
		bad := func(field, want string) error {
			return &providererrors.InvalidArgumentError{
				Field:   "providerOptions",
				Message: fmt.Sprintf("invalid %s provider options: %s must be %s", key, field, want),
			}
		}
		if x, set := m["outputDimensionality"]; set && x != nil && !isNumber(x) {
			return bad("outputDimensionality", "a number")
		}
		if x, set := m["taskType"]; set && x != nil {
			task, isString := x.(string)
			if !isString || !validVertexTaskType(task) {
				return bad("taskType", "one of SEMANTIC_SIMILARITY, CLASSIFICATION, CLUSTERING, RETRIEVAL_DOCUMENT, RETRIEVAL_QUERY, QUESTION_ANSWERING, FACT_VERIFICATION, CODE_RETRIEVAL_QUERY")
			}
		}
		if x, set := m["title"]; set && x != nil {
			if _, isString := x.(string); !isString {
				return bad("title", "a string")
			}
		}
		if x, set := m["autoTruncate"]; set && x != nil {
			if _, isBool := x.(bool); !isBool {
				return bad("autoTruncate", "a boolean")
			}
		}
		return nil
	}
	return nil
}

func validVertexTaskType(task string) bool {
	switch task {
	case "SEMANTIC_SIMILARITY", "CLASSIFICATION", "CLUSTERING", "RETRIEVAL_DOCUMENT", "RETRIEVAL_QUERY",
		"QUESTION_ANSWERING", "FACT_VERIFICATION", "CODE_RETRIEVAL_QUERY":
		return true
	}
	return false
}

func isNumber(v interface{}) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	}
	return false
}

// vertexEmbeddingError maps a failed predict/embedContent call: an HTTP
// failure gives the TS googleVertexFailedResponseHandler message (error.message
// of a Google error body, else the HTTP status text).
func vertexEmbeddingError(providerName string, err error) error {
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure is an APICallError as is
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return gemini.GoogleHTTPError(providerName, statusErr.StatusCode, statusErr.Headers, statusErr.Body, err)
	}
	return providererrors.NewProviderError(providerName, 0, "", "vertex embedding request failed: "+err.Error(), err)
}
