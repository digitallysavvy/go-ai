package bedrock

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// RerankingModel implements provider.RerankingModel for Amazon Bedrock.
type RerankingModel struct {
	provider *Provider
	modelID  string
	// region is the AWS region read when the model was created (TS
	// createRerankingModel loadSetting); "" reads it per request.
	region string
}

// RerankingOptions are Amazon Bedrock-specific reranking provider options.
type RerankingOptions struct {
	NextToken                       *string                `json:"nextToken,omitempty"`
	AdditionalModelRequestFields    map[string]interface{} `json:"additionalModelRequestFields,omitempty"`
	AdditionalModelRequestFieldsSet bool                   `json:"-"`
}

// NewRerankingModel creates a new Amazon Bedrock reranking model.
func NewRerankingModel(provider *Provider, modelID string) *RerankingModel {
	return &RerankingModel{provider: provider, modelID: modelID, region: provider.Region()}
}

func (m *RerankingModel) SpecificationVersion() string { return "v4" }

func (m *RerankingModel) Provider() string { return "amazon-bedrock" }

func (m *RerankingModel) ModelID() string { return m.modelID }

func (m *RerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("rerank options cannot be nil")
	}
	body, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}
	// JSON.stringify: a non-finite number in a JSON document or
	// additionalModelRequestFields is written as null (ruling 4).
	bodyBytes, err := jsonstr.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}
	baseURL, err := m.provider.agentRuntimeBaseURL()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/rerank", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	requestHeaders := map[string]string{}
	for k, v := range opts.Headers {
		requestHeaders[k] = v
	}
	m.provider.applyRequestHeaders(req, requestHeaders)
	if err := m.provider.authenticateRequest(ctx, req, bodyBytes); err != nil {
		return nil, err
	}

	resp, err := internalhttp.WithFetchRedirects(m.provider.Client().HTTPClient()).Do(req)
	if err != nil {
		// TS handleFetchError: a rejected fetch is returned unchanged, a
		// network failure is the retryable "Cannot connect to API" error.
		return nil, providerErrorFromTransport(req.Context(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := readJSONResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read rerank response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, bedrockAPIError(resp.StatusCode, respBody, providerutils.ExtractHeaders(resp.Header))
	}

	if err := bedrockValidator(zbRerank)(resp.StatusCode, resp.Header, respBody); err != nil {
		return nil, err
	}
	// The wire item has the exact keys of TS amazonBedrockRerankingResponseSchema
	// (types.RerankItem is untagged, so only case-insensitive matching filled it).
	var parsed struct {
		Results []bedrockRerankWireItem `json:"results"`
	}
	if err := jsonexact.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode rerank response: %w", err)
	}
	ranking := make([]types.RerankItem, len(parsed.Results))
	for i, r := range parsed.Results {
		ranking[i] = types.RerankItem{Index: r.Index, RelevanceScore: r.RelevanceScore}
	}
	var raw interface{}
	_ = jsonexact.Unmarshal(respBody, &raw)
	return &types.RerankResult{
		Ranking: ranking,
		Response: types.RerankResponse{
			Headers: map[string][]string(resp.Header),
			Body:    raw,
		},
	}, nil
}

// bedrockRerankWireItem is one TS amazonBedrockRerankingResponseSchema
// result, with the schema's exact keys.
type bedrockRerankWireItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevanceScore"`
}

func (m *RerankingModel) buildRequestBody(opts *provider.RerankOptions) (map[string]interface{}, error) {
	// Go-typed option literals are read through their JSON form.
	providerOptions := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)
	if err := validateRerankProviderOptions(providerOptions); err != nil {
		return nil, err
	}
	options := bedrockRerankingOptions(providerOptions)
	sources, err := bedrockRerankSources(opts.Documents)
	if err != nil {
		return nil, err
	}
	region := m.region
	if region == "" {
		region = m.provider.Region()
	}
	if region == "" {
		return nil, errRegionMissing()
	}
	modelConfiguration := map[string]interface{}{
		"modelArn": fmt.Sprintf("arn:aws:bedrock:%s::foundation-model/%s", region, m.modelID),
	}
	if options.AdditionalModelRequestFieldsSet {
		modelConfiguration["additionalModelRequestFields"] = options.AdditionalModelRequestFields
	}
	amazonConfig := map[string]interface{}{
		"modelConfiguration": modelConfiguration,
	}
	if opts.TopN != nil {
		amazonConfig["numberOfResults"] = *opts.TopN
	}
	body := map[string]interface{}{
		"queries": []map[string]interface{}{
			{
				"type":      "TEXT",
				"textQuery": map[string]interface{}{"text": opts.Query},
			},
		},
		"rerankingConfiguration": map[string]interface{}{
			"type":                          "BEDROCK_RERANKING_MODEL",
			"bedrockRerankingConfiguration": amazonConfig,
		},
		"sources": sources,
	}
	if options.NextToken != nil {
		body["nextToken"] = *options.NextToken
	}
	return body, nil
}

func bedrockRerankingOptions(providerOptions map[string]interface{}) RerankingOptions {
	var out RerankingOptions
	for _, key := range []string{"amazonBedrock", "bedrock"} {
		raw, ok := providerOptions[key]
		if !ok {
			continue
		}
		if values, ok := raw.(map[string]interface{}); ok {
			if nextToken, ok := values["nextToken"].(string); ok {
				out.NextToken = &nextToken
			}
			if additional, ok := values["additionalModelRequestFields"].(map[string]interface{}); ok {
				out.AdditionalModelRequestFields = additional
				out.AdditionalModelRequestFieldsSet = true
			}
			break
		}
	}
	return out
}

func bedrockRerankSources(documents interface{}) ([]map[string]interface{}, error) {
	switch docs := documents.(type) {
	case []string:
		sources := make([]map[string]interface{}, 0, len(docs))
		for _, doc := range docs {
			sources = append(sources, bedrockTextRerankSource(doc))
		}
		return sources, nil
	case []map[string]interface{}:
		sources := make([]map[string]interface{}, 0, len(docs))
		for _, doc := range docs {
			sources = append(sources, bedrockJSONRerankSource(doc))
		}
		return sources, nil
	case []interface{}:
		sources := make([]map[string]interface{}, 0, len(docs))
		for _, doc := range docs {
			if text, ok := doc.(string); ok {
				sources = append(sources, bedrockTextRerankSource(text))
			} else {
				sources = append(sources, bedrockJSONRerankSource(doc))
			}
		}
		return sources, nil
	default:
		return nil, fmt.Errorf("bedrock rerank documents must be []string, []map[string]interface{}, or []interface{}")
	}
}

func bedrockTextRerankSource(text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "INLINE",
		"inlineDocumentSource": map[string]interface{}{
			"type":         "TEXT",
			"textDocument": map[string]interface{}{"text": text},
		},
	}
}

func bedrockJSONRerankSource(value interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "INLINE",
		"inlineDocumentSource": map[string]interface{}{
			"type":         "JSON",
			"jsonDocument": value,
		},
	}
}
