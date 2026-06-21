package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// RerankingModel implements provider.RerankingModel for Amazon Bedrock.
type RerankingModel struct {
	provider *Provider
	modelID  string
}

// RerankingOptions are Amazon Bedrock-specific reranking provider options.
type RerankingOptions struct {
	NextToken                       *string                `json:"nextToken,omitempty"`
	AdditionalModelRequestFields    map[string]interface{} `json:"additionalModelRequestFields,omitempty"`
	AdditionalModelRequestFieldsSet bool                   `json:"-"`
}

// NewRerankingModel creates a new Amazon Bedrock reranking model.
func NewRerankingModel(provider *Provider, modelID string) *RerankingModel {
	return &RerankingModel{provider: provider, modelID: modelID}
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
	bodyBytes, err := json.Marshal(body)
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

	resp, err := m.provider.Client().HTTPClient().Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError("amazon-bedrock", 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read rerank response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AWS Bedrock rerank API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Results []types.RerankItem `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode rerank response: %w", err)
	}
	var raw interface{}
	_ = json.Unmarshal(respBody, &raw)
	return &types.RerankResult{
		Ranking: parsed.Results,
		Response: types.RerankResponse{
			Headers: map[string][]string(resp.Header),
			Body:    raw,
		},
	}, nil
}

func (m *RerankingModel) buildRequestBody(opts *provider.RerankOptions) (map[string]interface{}, error) {
	options := bedrockRerankingOptions(opts.ProviderOptions)
	sources, err := bedrockRerankSources(opts.Documents)
	if err != nil {
		return nil, err
	}
	region := m.provider.Region()
	if region == "" {
		return nil, fmt.Errorf("AWS region is required: set Region or AWS_REGION")
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
			"type":                                "BEDROCK_RERANKING_MODEL",
			"amazonBedrockRerankingConfiguration": amazonConfig,
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
