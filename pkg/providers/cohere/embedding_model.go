package cohere

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Cohere
type EmbeddingModel struct {
	provider *Provider
	modelID  string
	options  EmbeddingOptions
}

// NewEmbeddingModel creates a new Cohere embedding model
func NewEmbeddingModel(provider *Provider, modelID string, options ...EmbeddingOptions) *EmbeddingModel {
	opts := DefaultEmbeddingOptions()
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
	return "cohere.textEmbedding"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Cohere supports 96 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 96
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
	// TS parseProviderOptions({provider: 'cohere', schema: cohereEmbeddingModelOptions})
	// runs before the too-many-values check.
	var parsed map[string]interface{}
	if opts != nil {
		var err error
		parsed, err = providerutils.ParseProviderOptions("cohere", opts.ProviderOptions, cohereEmbeddingOptionsSchema)
		if err != nil {
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
	if err := m.options.Validate(); err != nil {
		return nil, err
	}

	// Per-call options win over the model options.
	embeddingType := m.options.EmbeddingType
	inputType := m.options.InputType
	truncate := m.options.Truncate
	var outputDimension *int
	if m.options.OutputDimension != nil {
		d := int(*m.options.OutputDimension)
		outputDimension = &d
	}
	if v, ok := parsed["embeddingType"].(string); ok {
		embeddingType = EmbeddingType(v)
	}
	if v, ok := parsed["inputType"].(string); ok {
		inputType = InputType(v)
	}
	if v, ok := parsed["truncate"].(string); ok {
		truncate = TruncateMode(v)
	}
	if v, ok := parsed["outputDimension"]; ok {
		d := int(reflect.ValueOf(v).Convert(reflect.TypeOf(float64(0))).Float())
		outputDimension = &d
	}
	if embeddingType == "" {
		embeddingType = EmbeddingTypeFloat
	}
	if inputType == "" {
		inputType = InputTypeSearchQuery
	}

	reqBody := map[string]interface{}{
		"texts":           inputs,
		"model":           m.modelID,
		"embedding_types": []string{string(embeddingType)},
		"input_type":      string(inputType),
	}
	if truncate != "" {
		reqBody["truncate"] = string(truncate)
	}
	if outputDimension != nil {
		reqBody["output_dimension"] = *outputDimension
	} else if opts != nil && opts.Dimensions != nil {
		// Top-level dimensions (TS #22338); the provider option wins.
		reqBody["output_dimension"] = *opts.Dimensions
	}

	var response cohereEmbedResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.CohereEmbeddingResponseValidator(m.Provider(), string(embeddingType)),
		Method:           http.MethodPost,
		Path:             "/embed",
		Body:             reqBody,
		Headers:          optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
	}
	embeddings, err := response.embeddingsFor(embeddingType)
	if err != nil {
		return nil, providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
	}
	// TS response.body is the raw parsed JSON, not the schema-filtered value.
	var rawBody interface{}
	if err := json.Unmarshal(httpResp.Body, &rawBody); err != nil {
		rawBody = response
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			Tokens:      float64(response.Meta.BilledUnits.InputTokens),
			InputTokens: response.Meta.BilledUnits.InputTokens,
			TotalTokens: response.Meta.BilledUnits.InputTokens,
		},
		Responses: []types.EmbeddingResponse{{Headers: providerutils.ExtractHeaders(httpResp.Headers), Body: rawBody}},
	}, nil
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

type cohereEmbedResponse struct {
	// Embeddings is either an object keyed by embedding type (when
	// embedding_types is requested) or a plain float array.
	Embeddings json.RawMessage `json:"embeddings"`
	Meta       struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}

// embeddingsFor extracts the embeddings of the requested type, ignoring any
// other formats present in the response.
func (r cohereEmbedResponse) embeddingsFor(t EmbeddingType) ([][]float64, error) {
	raw := bytes.TrimSpace(r.Embeddings)
	var byType map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byType); err != nil {
		return nil, fmt.Errorf("invalid cohere embeddings response: %w", err)
	}
	sel, ok := byType[string(t)]
	if !ok {
		return nil, fmt.Errorf("invalid cohere embeddings response: missing %s embeddings", t)
	}
	var out [][]float64
	if err := json.Unmarshal(sel, &out); err != nil {
		return nil, fmt.Errorf("invalid cohere embeddings response: %w", err)
	}
	return out, nil
}
