package alibaba

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const alibabaMaxEmbeddingsPerCall = 10

// EmbeddingModel implements Alibaba DashScope text embeddings.
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Alibaba embedding model.
func NewEmbeddingModel(provider *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{provider: provider, modelID: modelID}
}

func (m *EmbeddingModel) SpecificationVersion() string { return "v4" }
func (m *EmbeddingModel) Provider() string             { return "alibaba.embedding" }
func (m *EmbeddingModel) ModelID() string              { return m.modelID }
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int    { return alibabaMaxEmbeddingsPerCall }
func (m *EmbeddingModel) SupportsParallelCalls() bool  { return false }

// DoEmbed embeds a single text value.
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	result, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned from Alibaba")
	}
	out := &types.EmbeddingResult{
		Embedding:        result.Embeddings[0],
		Usage:            result.Usage,
		Warnings:         result.Warnings,
		ProviderMetadata: result.ProviderMetadata,
	}
	if len(result.Responses) > 0 {
		out.Response = result.Responses[0]
	}
	return out, nil
}

// DoEmbedMany embeds multiple text values in one DashScope request.
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) > alibabaMaxEmbeddingsPerCall {
		return nil, &providererrors.TooManyEmbeddingValuesForCallError{
			Provider:             m.Provider(),
			ModelID:              m.modelID,
			MaxEmbeddingsPerCall: alibabaMaxEmbeddingsPerCall,
			Values:               inputs,
		}
	}
	alibabaOpts, err := extractAlibabaEmbeddingOptions(opts)
	if err != nil {
		return nil, err
	}
	if alibabaOpts.OutputType == AlibabaEmbeddingOutputSparse {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "Alibaba embedding outputType 'sparse'",
			Message:       "Alibaba embedding outputType 'sparse' is not supported because AI SDK embeddings require dense number arrays. Use 'dense' or 'dense&sparse' instead.",
		}
	}

	body := map[string]interface{}{
		"model": m.modelID,
		"input": map[string]interface{}{"texts": inputs},
	}
	parameters := map[string]interface{}{}
	if alibabaOpts.TextType != "" {
		parameters["text_type"] = alibabaOpts.TextType
	}
	if alibabaOpts.Dimension != nil {
		parameters["dimension"] = *alibabaOpts.Dimension
	}
	if alibabaOpts.OutputType != "" {
		parameters["output_type"] = alibabaOpts.OutputType
	}
	body["parameters"] = parameters

	var response alibabaTextEmbeddingResponse
	httpResp, err := m.provider.embeddingClient.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/services/embeddings/text-embedding/text-embedding",
		Body:    body,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	responseBody, err := parseAlibabaEmbeddingRawBody(httpResp.Body)
	if err != nil {
		return nil, err
	}

	sort.Slice(response.Output.Embeddings, func(i, j int) bool {
		return response.Output.Embeddings[i].TextIndex < response.Output.Embeddings[j].TextIndex
	})

	embeddings := make([][]float64, len(response.Output.Embeddings))
	sparse := make([]map[string]interface{}, 0)
	for i, item := range response.Output.Embeddings {
		embeddings[i] = item.Embedding
		if len(item.SparseEmbedding) > 0 {
			sparse = append(sparse, map[string]interface{}{
				"textIndex":       item.TextIndex,
				"sparseEmbedding": normalizeAlibabaSparseEmbedding(item.SparseEmbedding),
			})
		}
	}

	var inputTokens int
	if response.Usage != nil {
		inputTokens = response.Usage.TotalTokens
	}
	var providerMetadata map[string]interface{}
	if len(sparse) > 0 {
		providerMetadata = map[string]interface{}{
			"alibaba": map[string]interface{}{"sparseEmbeddings": sparse},
		}
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(inputTokens),
			InputTokens: inputTokens,
			TotalTokens: inputTokens,
		},
		Responses: []types.EmbeddingResponse{{
			Headers: providerutils.ExtractHeaders(httpResp.Headers),
			Body:    responseBody,
		}},
		ProviderMetadata: providerMetadata,
	}, nil
}

func (m *EmbeddingModel) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		}
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil {
			if payload.Message != "" {
				message = payload.Message
			}
			code = payload.Code
		}
		providerErr := providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		return providerErr
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

func extractAlibabaEmbeddingOptions(opts *provider.EmbedModelOptions) (AlibabaEmbeddingModelOptions, error) {
	if opts == nil || opts.ProviderOptions == nil {
		return AlibabaEmbeddingModelOptions{}, nil
	}
	raw, ok := opts.ProviderOptions["alibaba"]
	if !ok || raw == nil {
		return AlibabaEmbeddingModelOptions{}, nil
	}
	switch v := raw.(type) {
	case AlibabaEmbeddingModelOptions:
		return wrapAlibabaEmbeddingOptionsValidation(validateAlibabaEmbeddingOptions(v))
	case *AlibabaEmbeddingModelOptions:
		if v == nil {
			return AlibabaEmbeddingModelOptions{}, nil
		}
		return wrapAlibabaEmbeddingOptionsValidation(validateAlibabaEmbeddingOptions(*v))
	case map[string]interface{}:
		return wrapAlibabaEmbeddingOptionsValidation(parseAlibabaEmbeddingOptionMap(v))
	default:
		return AlibabaEmbeddingModelOptions{}, invalidAlibabaEmbeddingOptionsError(fmt.Errorf("expected object"))
	}
}

func wrapAlibabaEmbeddingOptionsValidation(opts AlibabaEmbeddingModelOptions, err error) (AlibabaEmbeddingModelOptions, error) {
	if err != nil {
		return opts, invalidAlibabaEmbeddingOptionsError(err)
	}
	return opts, nil
}

func invalidAlibabaEmbeddingOptionsError(cause error) error {
	return &providererrors.InvalidArgumentError{
		Field:   "providerOptions",
		Message: "invalid alibaba provider options",
		Cause:   cause,
	}
}

func parseAlibabaEmbeddingOptionMap(raw map[string]interface{}) (AlibabaEmbeddingModelOptions, error) {
	var out AlibabaEmbeddingModelOptions
	if v, ok := raw["textType"]; ok {
		textType, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("invalid alibaba textType: expected string")
		}
		out.TextType = textType
	}
	if v, ok := raw["dimension"]; ok {
		switch n := v.(type) {
		case int:
			dimension := float64(n)
			out.Dimension = &dimension
		case int64:
			dimension := float64(n)
			out.Dimension = &dimension
		case float64:
			dimension := n
			out.Dimension = &dimension
		default:
			return out, fmt.Errorf("invalid alibaba dimension: expected number")
		}
	}
	if v, ok := raw["outputType"]; ok {
		outputType, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("invalid alibaba outputType: expected string")
		}
		out.OutputType = outputType
	}
	return validateAlibabaEmbeddingOptions(out)
}

func validateAlibabaEmbeddingOptions(opts AlibabaEmbeddingModelOptions) (AlibabaEmbeddingModelOptions, error) {
	if opts.TextType != "" && opts.TextType != "query" && opts.TextType != "document" {
		return opts, fmt.Errorf("invalid alibaba textType %q: expected \"query\" or \"document\"", opts.TextType)
	}
	switch opts.OutputType {
	case "", AlibabaEmbeddingOutputDense, AlibabaEmbeddingOutputSparse, AlibabaEmbeddingOutputDenseSparse:
		return opts, nil
	default:
		return opts, fmt.Errorf("invalid alibaba outputType %q: expected \"dense\", \"sparse\", or \"dense&sparse\"", opts.OutputType)
	}
}

func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

func parseAlibabaEmbeddingRawBody(body []byte) (interface{}, error) {
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Alibaba embedding response metadata: %w", err)
	}
	return raw, nil
}

type alibabaTextEmbeddingResponse struct {
	Output struct {
		Embeddings []struct {
			Embedding       []float64                `json:"embedding"`
			TextIndex       int                      `json:"text_index"`
			SparseEmbedding []map[string]interface{} `json:"sparse_embedding,omitempty"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage *struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func normalizeAlibabaSparseEmbedding(items []map[string]interface{}) []map[string]interface{} {
	normalized := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out := map[string]interface{}{}
		if index, ok := item["index"]; ok {
			out["index"] = index
		}
		if value, ok := item["value"]; ok {
			out["value"] = value
		}
		if token, ok := item["token"]; ok {
			out["token"] = token
		}
		normalized = append(normalized, out)
	}
	return normalized
}
