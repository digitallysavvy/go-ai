package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for AWS Bedrock
type EmbeddingModel struct {
	provider *Provider
	modelID  string
	options  *EmbeddingOptions
}

// NewEmbeddingModel creates a new AWS Bedrock embedding model
func NewEmbeddingModel(provider *Provider, modelID string, options ...*EmbeddingOptions) *EmbeddingModel {
	var opts *EmbeddingOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
		options:  opts,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return "amazon-bedrock"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Bedrock only supports 1 embedding per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 1
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	// Determine model type and construct request body accordingly
	var reqBody map[string]interface{}
	embeddingOptions := m.options
	if opts != nil {
		embeddingOptions = mergeEmbeddingOptions(embeddingOptions, bedrockEmbeddingOptions(opts.ProviderOptions))
	}

	isNovaModel := strings.HasPrefix(m.modelID, "amazon.nova-") && strings.Contains(m.modelID, "embed")
	isCohereModel := bedrockIsCohereEmbeddingModel(m.modelID)

	switch {
	case isNovaModel:
		novaOpts := NovaEmbeddingOptions{}
		if embeddingOptions != nil && embeddingOptions.NovaOptions != nil {
			if err := embeddingOptions.NovaOptions.Validate(); err != nil {
				return nil, err
			}
			novaOpts = *embeddingOptions.NovaOptions
		}
		embeddingPurpose := novaOpts.EmbeddingPurpose
		if embeddingPurpose == "" {
			embeddingPurpose = "GENERIC_INDEX"
		}
		embeddingDimension := 1024
		if novaOpts.EmbeddingDimension != nil {
			embeddingDimension = *novaOpts.EmbeddingDimension
		}
		truncate := string(novaOpts.Truncate)
		if truncate == "" {
			truncate = "END"
		}
		reqBody = map[string]interface{}{
			"taskType": "SINGLE_EMBEDDING",
			"singleEmbeddingParams": map[string]interface{}{
				"embeddingPurpose":   embeddingPurpose,
				"embeddingDimension": embeddingDimension,
				"text": map[string]interface{}{
					"truncationMode": truncate,
					"value":          input,
				},
			},
		}
	case isCohereModel:
		// Validate Cohere options if provided
		if embeddingOptions != nil && embeddingOptions.CohereOptions != nil {
			if err := embeddingOptions.CohereOptions.Validate(); err != nil {
				return nil, err
			}
		}

		// Build Cohere request
		cohereOpts := DefaultCohereEmbeddingOptions()
		if embeddingOptions != nil && embeddingOptions.CohereOptions != nil {
			cohereOpts = *embeddingOptions.CohereOptions
		}

		reqBody = map[string]interface{}{
			"texts":      []string{input},
			"input_type": string(cohereOpts.InputType),
		}

		if cohereOpts.OutputDimension != nil {
			reqBody["output_dimension"] = int(*cohereOpts.OutputDimension)
		}

		if cohereOpts.Truncate != "" {
			reqBody["truncate"] = string(cohereOpts.Truncate)
		}
	default:
		// Titan or other models
		reqBody = map[string]interface{}{
			"inputText": input,
		}

		// Add Titan-specific options if provided
		if embeddingOptions != nil && embeddingOptions.TitanOptions != nil {
			if embeddingOptions.TitanOptions.Dimensions != nil {
				reqBody["dimensions"] = *embeddingOptions.TitanOptions.Dimensions
			}
			if embeddingOptions.TitanOptions.Normalize != nil {
				reqBody["normalize"] = *embeddingOptions.TitanOptions.Normalize
			}
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("/model/%s/invoke", url.PathEscape(m.modelID))
	baseURL, err := m.provider.runtimeBaseURL()
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s%s", baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Forward caller-supplied headers.
	requestHeaders := map[string]string{}
	if opts != nil {
		for k, v := range opts.Headers {
			requestHeaders[k] = v
		}
	}
	m.provider.applyRequestHeaders(req, requestHeaders)

	if err := m.provider.authenticateRequest(ctx, req, bodyBytes); err != nil {
		return nil, err
	}

	// Make the request using provider-scoped transport.
	resp, err := m.provider.Client().HTTPClient().Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError("amazon-bedrock", 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("AWS Bedrock API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response based on model type
	var embedding []float64
	var inputTokens int

	var parsed struct {
		Embedding           []float64       `json:"embedding"`
		InputTextTokenCount *int            `json:"inputTextTokenCount"`
		InputTokenCount     *int            `json:"inputTokenCount"`
		Embeddings          json.RawMessage `json:"embeddings"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	switch {
	case parsed.Embedding != nil:
		// Titan response.
		embedding = parsed.Embedding
		if parsed.InputTextTokenCount != nil {
			inputTokens = *parsed.InputTextTokenCount
		}
	case len(parsed.Embeddings) > 0:
		var nova []struct {
			EmbeddingType string    `json:"embeddingType"`
			Embedding     []float64 `json:"embedding"`
		}
		if err := json.Unmarshal(parsed.Embeddings, &nova); err == nil && len(nova) > 0 && nova[0].EmbeddingType != "" {
			embedding = nova[0].Embedding
			if parsed.InputTokenCount != nil {
				inputTokens = *parsed.InputTokenCount
			}
			break
		}

		var cohereV3 [][]float64
		if err := json.Unmarshal(parsed.Embeddings, &cohereV3); err == nil && len(cohereV3) > 0 {
			embedding = cohereV3[0]
			break
		}

		var cohereV4 struct {
			Float [][]float64 `json:"float"`
		}
		if err := json.Unmarshal(parsed.Embeddings, &cohereV4); err == nil && len(cohereV4.Float) > 0 {
			embedding = cohereV4.Float[0]
			break
		}
	}
	if embedding == nil {
		return nil, fmt.Errorf("no embeddings in response")
	}
	tokens := float64(inputTokens)
	if isCohereModel {
		if headerTokens, err := strconv.Atoi(resp.Header.Get("x-amzn-bedrock-input-token-count")); err == nil {
			inputTokens = headerTokens
			tokens = float64(headerTokens)
		} else {
			tokens = math.NaN()
		}
	}

	return &types.EmbeddingResult{
		Embedding: embedding,
		Usage: types.EmbeddingUsage{
			Tokens:      tokens,
			InputTokens: inputTokens,
			TotalTokens: inputTokens,
		},
		Warnings: []types.Warning{},
		Response: types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(resp.Header)},
	}, nil
}

func bedrockEmbeddingOptions(providerOptions map[string]interface{}) *EmbeddingOptions {
	for _, key := range []string{"amazonBedrock", "bedrock"} {
		raw, ok := providerOptions[key]
		if !ok {
			continue
		}
		values, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}
		out := &EmbeddingOptions{}
		if dimensions, ok := intOption(values["dimensions"]); ok {
			out.TitanOptions = ensureTitanOptions(out.TitanOptions)
			out.TitanOptions.Dimensions = &dimensions
		}
		if normalize, ok := values["normalize"].(bool); ok {
			out.TitanOptions = ensureTitanOptions(out.TitanOptions)
			out.TitanOptions.Normalize = &normalize
		}
		if embeddingDimension, ok := intOption(values["embeddingDimension"]); ok {
			out.NovaOptions = ensureNovaOptions(out.NovaOptions)
			out.NovaOptions.EmbeddingDimension = &embeddingDimension
		}
		if embeddingPurpose, ok := values["embeddingPurpose"].(string); ok {
			out.NovaOptions = ensureNovaOptions(out.NovaOptions)
			out.NovaOptions.EmbeddingPurpose = embeddingPurpose
		}
		if inputType, ok := values["inputType"].(string); ok {
			out.CohereOptions = ensureCohereOptions(out.CohereOptions)
			out.CohereOptions.InputType = cohere.InputType(inputType)
		}
		if truncate, ok := values["truncate"].(string); ok {
			out.CohereOptions = ensureCohereOptions(out.CohereOptions)
			out.CohereOptions.Truncate = cohere.TruncateMode(truncate)
			out.NovaOptions = ensureNovaOptions(out.NovaOptions)
			out.NovaOptions.Truncate = cohere.TruncateMode(truncate)
		}
		if outputDimension, ok := intOption(values["outputDimension"]); ok {
			dimension := cohere.OutputDimension(outputDimension)
			out.CohereOptions = ensureCohereOptions(out.CohereOptions)
			out.CohereOptions.OutputDimension = &dimension
		}
		return out
	}
	return nil
}

func mergeEmbeddingOptions(base, override *EmbeddingOptions) *EmbeddingOptions {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}
	merged := *base
	if override.CohereOptions != nil {
		merged.CohereOptions = override.CohereOptions
	}
	if override.TitanOptions != nil {
		merged.TitanOptions = override.TitanOptions
	}
	if override.NovaOptions != nil {
		merged.NovaOptions = override.NovaOptions
	}
	return &merged
}

func ensureCohereOptions(options *CohereEmbeddingOptions) *CohereEmbeddingOptions {
	if options != nil {
		return options
	}
	defaults := DefaultCohereEmbeddingOptions()
	return &defaults
}

func ensureTitanOptions(options *TitanEmbeddingOptions) *TitanEmbeddingOptions {
	if options != nil {
		return options
	}
	return &TitanEmbeddingOptions{}
}

func ensureNovaOptions(options *NovaEmbeddingOptions) *NovaEmbeddingOptions {
	if options != nil {
		return options
	}
	return &NovaEmbeddingOptions{}
}

func intOption(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func bedrockIsCohereEmbeddingModel(modelID string) bool {
	if strings.HasPrefix(modelID, "cohere.embed-") {
		return true
	}
	parts := strings.SplitN(modelID, ".", 2)
	return len(parts) == 2 && strings.HasPrefix(parts[1], "cohere.embed-")
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	var embeddings [][]float64
	var totalTokens int
	responses := make([]types.EmbeddingResponse, 0, len(inputs))

	// Process each input individually
	// Bedrock embeddings typically don't support batch processing
	for _, input := range inputs {
		result, err := m.DoEmbed(ctx, input, opts)
		if err != nil {
			return nil, err
		}

		embeddings = append(embeddings, result.Embedding)
		totalTokens += result.Usage.InputTokens
		responses = append(responses, result.Response)
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(totalTokens),
			InputTokens: totalTokens,
			TotalTokens: totalTokens,
		},
		Warnings:  []types.Warning{},
		Responses: responses,
	}, nil
}
