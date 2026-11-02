package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call.
// Cohere embedding models on Bedrock support batching up to 96 texts per
// call; Titan and Nova models support only 1. Ports TS
// amazon-bedrock-embedding-model.ts#maxEmbeddingsPerCall.
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	if m.modelFamily() == EmbeddingModelFamilyCohere {
		return 96
	}
	return 1
}

// modelFamily resolves the effective embedding model family: an explicit
// EmbeddingOptions.ModelFamily override wins, otherwise it is detected from
// the model ID. Ports TS detectEmbeddingModelFamily.
func (m *EmbeddingModel) modelFamily() EmbeddingModelFamily {
	if m.options != nil && m.options.ModelFamily != "" {
		return EmbeddingModelFamily(m.options.ModelFamily)
	}
	return detectBedrockEmbeddingModelFamily(m.modelID)
}

func detectBedrockEmbeddingModelFamily(modelID string) EmbeddingModelFamily {
	switch {
	case strings.Contains(modelID, "amazon.nova-") && strings.Contains(modelID, "embed"):
		return EmbeddingModelFamilyNova
	case bedrockIsCohereEmbeddingModel(modelID):
		return EmbeddingModelFamilyCohere
	default:
		return EmbeddingModelFamilyTitan
	}
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	result, err := m.embedValues(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		// TS embed() rejects a response without embeddings with
		// InvalidResponseDataError("No embedding generated.", data: []).
		return nil, providererrors.NewInvalidResponseDataError([][]float64{}, "No embedding generated.")
	}
	return &types.EmbeddingResult{
		Embedding: result.Embeddings[0],
		Usage:     result.Usage,
		Warnings:  []types.Warning{},
		Response:  result.Responses[0],
	}, nil
}

// embedValues is TS AmazonBedrockEmbeddingModel.doEmbed: one InvokeModel
// call for values (a single value for Titan and Nova, up to 96 for Cohere).
// The embeddings are returned as the response carries them, without
// comparing their number with the values (embed/embedMany check that).
func (m *EmbeddingModel) embedValues(ctx context.Context, values []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	embeddingOptions := m.options
	if opts != nil {
		if err := validateEmbeddingProviderOptions(opts.ProviderOptions); err != nil {
			return nil, err
		}
		embeddingOptions = mergeEmbeddingOptions(embeddingOptions, bedrockEmbeddingOptions(opts.ProviderOptions))
	}

	var reqBody map[string]interface{}
	switch m.modelFamily() {
	case EmbeddingModelFamilyNova:
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
		} else if opts != nil && opts.Dimensions != nil {
			// Top-level dimensions (TS #22338); the provider option wins.
			embeddingDimension = *opts.Dimensions
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
					"value":          values[0],
				},
			},
		}
	case EmbeddingModelFamilyCohere:
		if embeddingOptions != nil && embeddingOptions.CohereOptions != nil {
			if err := embeddingOptions.CohereOptions.Validate(); err != nil {
				return nil, err
			}
		}
		cohereOpts := DefaultCohereEmbeddingOptions()
		if embeddingOptions != nil && embeddingOptions.CohereOptions != nil {
			cohereOpts = *embeddingOptions.CohereOptions
		}
		reqBody = map[string]interface{}{
			"texts":      values,
			"input_type": string(cohereOpts.InputType),
		}
		if cohereOpts.OutputDimension != nil {
			reqBody["output_dimension"] = int(*cohereOpts.OutputDimension)
		} else if opts != nil && opts.Dimensions != nil {
			reqBody["output_dimension"] = *opts.Dimensions
		}
		if cohereOpts.Truncate != "" {
			reqBody["truncate"] = string(cohereOpts.Truncate)
		}
	default:
		// Titan or other models
		reqBody = map[string]interface{}{
			"inputText": values[0],
		}
		if opts != nil && opts.Dimensions != nil {
			// Top-level dimensions (TS #22338); the Titan option below wins.
			reqBody["dimensions"] = *opts.Dimensions
		}
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

	baseURL, err := m.provider.runtimeBaseURL()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	invokeURL, _, err := bedrockModelURL(baseURL, m.modelID, "/invoke")
	if err != nil {
		return nil, err
	}
	req.URL = invokeURL
	req.Host = invokeURL.Host

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

	respBody, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, bedrockAPIError(resp.StatusCode, respBody, providerutils.ExtractHeaders(resp.Header))
	}

	if err := bedrockValidator(zbEmbedding)(resp.StatusCode, resp.Header, respBody); err != nil {
		return nil, err
	}

	embeddings, tokens, err := parseBedrockEmbeddingResponse(respBody, resp.Header)
	if err != nil {
		return nil, err
	}
	count := bedrockEmbeddingTokenCount(tokens)
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		// TS always returns `usage: { tokens }` (NaN when the token count
		// comes from a missing header), so the usage is always Reported.
		Usage: types.EmbeddingUsage{
			Tokens:      tokens,
			InputTokens: count,
			TotalTokens: count,
			Reported:    true,
		},
		Warnings:  []types.Warning{},
		Responses: []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(resp.Header)}},
	}, nil
}

// parseBedrockEmbeddingResponse maps a body that passed zbEmbedding as TS
// does after AmazonBedrockEmbeddingResponseSchema (a union whose first
// matching member wins and strips the other keys):
//   - Titan ({embedding, inputTextTokenCount}): one embedding, the body count.
//   - Nova (an `embeddings` array of {embeddingType, embedding} objects, which
//     includes an empty array): the first embedding (none for an empty
//     array) and inputTokenCount when the body has it.
//   - Cohere v3 (an array of arrays) and v4 ({float}): every embedding.
//
// Without a body count the tokens are TS
// Number(responseHeaders['x-amzn-bedrock-input-token-count']): NaN when the
// header is missing or not a number.
func parseBedrockEmbeddingResponse(body []byte, header http.Header) ([][]float64, float64, error) {
	var parsed struct {
		Embedding           []float64       `json:"embedding"`
		InputTextTokenCount *float64        `json:"inputTextTokenCount"`
		InputTokenCount     *float64        `json:"inputTokenCount"`
		Embeddings          json.RawMessage `json:"embeddings"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, fmt.Errorf("failed to decode embedding response: %w", err)
	}
	if parsed.Embedding != nil && parsed.InputTextTokenCount != nil {
		return [][]float64{parsed.Embedding}, *parsed.InputTextTokenCount, nil
	}
	raw := bytes.TrimSpace(parsed.Embeddings)
	if len(raw) > 0 && raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, 0, fmt.Errorf("failed to decode embedding response: %w", err)
		}
		if len(items) == 0 || bytes.HasPrefix(bytes.TrimSpace(items[0]), []byte("{")) {
			// Nova member.
			embeddings := [][]float64{}
			if len(items) > 0 {
				var first struct {
					Embedding []float64 `json:"embedding"`
				}
				if err := json.Unmarshal(items[0], &first); err != nil {
					return nil, 0, fmt.Errorf("failed to decode embedding response: %w", err)
				}
				embeddings = [][]float64{first.Embedding}
			}
			if parsed.InputTokenCount != nil {
				return embeddings, *parsed.InputTokenCount, nil
			}
			return embeddings, bedrockHeaderTokenCount(header), nil
		}
		var cohereV3 [][]float64
		if err := json.Unmarshal(raw, &cohereV3); err != nil {
			return nil, 0, fmt.Errorf("failed to decode embedding response: %w", err)
		}
		return cohereV3, bedrockHeaderTokenCount(header), nil
	}
	var cohereV4 struct {
		Float [][]float64 `json:"float"`
	}
	if err := json.Unmarshal(raw, &cohereV4); err != nil {
		return nil, 0, fmt.Errorf("failed to decode embedding response: %w", err)
	}
	embeddings := cohereV4.Float
	if embeddings == nil {
		embeddings = [][]float64{}
	}
	return embeddings, bedrockHeaderTokenCount(header), nil
}

// bedrockHeaderTokenCount is TS Number(responseHeaders[
// 'x-amzn-bedrock-input-token-count']): NaN for a missing header (Number of
// undefined) or a value that is not a JS number, 0 for an empty value.
func bedrockHeaderTokenCount(header http.Header) float64 {
	values := header.Values("x-amzn-bedrock-input-token-count")
	if len(values) == 0 {
		return math.NaN()
	}
	// fetch Headers.get joins repeated values with ", ".
	n, ok := providerutils.JSNumber(strings.Join(values, ", "))
	if !ok {
		return math.NaN()
	}
	return n
}

// bedrockEmbeddingTokenCount is the integral InputTokens/TotalTokens for a
// token count: fractions truncate, NaN and infinities are 0.
func bedrockEmbeddingTokenCount(tokens float64) int {
	switch {
	case math.IsNaN(tokens), math.IsInf(tokens, 0):
		return 0
	case tokens >= math.MaxInt32:
		if tokens >= float64(math.MaxInt) {
			return math.MaxInt
		}
	case tokens <= math.MinInt32:
		if tokens <= float64(math.MinInt) {
			return math.MinInt
		}
	}
	return int(tokens)
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
	// TS modelId.includes('cohere.embed-'): cross-region inference profile ids
	// (us.cohere.embed-v4:0, global.cohere.embed-v4:0) are detected too.
	return strings.Contains(modelID, "cohere.embed-")
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	// Cohere embedding models on Bedrock accept up to 96 texts in a single
	// `texts: [...]` request; Titan and Nova models one. ai.EmbedMany splits
	// the values by MaxEmbeddingsPerCall; DoEmbedMany also chunks so a direct
	// call with more values than one request takes still works.
	max := m.MaxEmbeddingsPerCall()
	embeddings := [][]float64{}
	var totalTokens int
	// TS sums usage.tokens, so a NaN (missing token-count header) in any
	// batch makes the total NaN, which telemetry drops. Tokens keeps the
	// v0.5 integral sum; a NaN batch leaves the usage unreported instead.
	reported := true
	responses := make([]types.EmbeddingResponse, 0, (len(inputs)+max-1)/max)
	for start := 0; start < len(inputs); start += max {
		end := start + max
		if end > len(inputs) {
			end = len(inputs)
		}
		result, err := m.embedValues(ctx, inputs[start:end], opts)
		if err != nil {
			return nil, err
		}
		// The embeddings are kept as sent; EmbedMany compares their number
		// with the values (TS "Expected N embeddings, but received M.").
		embeddings = append(embeddings, result.Embeddings...)
		totalTokens += result.Usage.InputTokens
		if math.IsNaN(result.Usage.Tokens) {
			reported = false
		}
		responses = append(responses, result.Responses...)
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(totalTokens),
			InputTokens: totalTokens,
			TotalTokens: totalTokens,
			Reported:    reported,
		},
		Warnings:  []types.Warning{},
		Responses: responses,
	}, nil
}
