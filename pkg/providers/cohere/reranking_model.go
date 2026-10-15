package cohere

import (
	"context"
	"encoding/json"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// RerankingModel implements the provider.RerankingModel interface for Cohere
type RerankingModel struct {
	provider *Provider
	modelID  string
}

// NewRerankingModel creates a new Cohere reranking model
func NewRerankingModel(provider *Provider, modelID string) *RerankingModel {
	return &RerankingModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *RerankingModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *RerankingModel) Provider() string {
	return "cohere.reranking"
}

// ModelID returns the model ID
func (m *RerankingModel) ModelID() string {
	return m.modelID
}

// DoRerank performs document reranking (TS CohereRerankingModel#doRerank).
func (m *RerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	if opts == nil {
		opts = &provider.RerankOptions{}
	}
	// TS parseProviderOptions({provider: 'cohere', schema: cohereRerankingModelOptionsSchema}).
	if _, err := providerutils.ParseProviderOptions("cohere", opts.ProviderOptions, cohereRerankingOptionsSchema); err != nil {
		return nil, err
	}
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts)

	var response cohereRerankResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{Method: http.MethodPost, Path: "/rerank", Body: reqBody, Headers: opts.Headers}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	result := m.convertResponse(response)
	result.Warnings = warnings
	result.Response.Headers = map[string][]string(httpResp.Headers)
	var raw interface{}
	if json.Unmarshal(httpResp.Body, &raw) == nil {
		result.Response.Body = raw
	}
	return result, nil
}

// buildRequestBodyWithWarnings mirrors the TS request: object documents are
// converted to JSON strings (with a compatibility warning) and the
// providerOptions.cohere maxTokensPerDoc/priority options are forwarded.
func (m *RerankingModel) buildRequestBodyWithWarnings(opts *provider.RerankOptions) (map[string]interface{}, []types.Warning) {
	body := map[string]interface{}{
		"model": m.modelID,
		"query": opts.Query,
	}
	var warnings []types.Warning

	switch docs := opts.Documents.(type) {
	case []string:
		body["documents"] = docs
	case nil:
		body["documents"] = []string{}
	default:
		var items []interface{}
		switch typed := docs.(type) {
		case []map[string]interface{}:
			for _, d := range typed {
				items = append(items, d)
			}
		case []interface{}:
			items = typed
		}
		strs := make([]string, 0, len(items))
		allText := true
		for _, item := range items {
			if s, ok := item.(string); ok {
				strs = append(strs, s)
				continue
			}
			allText = false
			raw, _ := json.Marshal(item)
			strs = append(strs, string(raw))
		}
		if !allText {
			warnings = append(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "object documents",
				Details: "Object documents are converted to strings.",
			})
		}
		body["documents"] = strs
	}

	// TS sends `top_n: topN` whenever it is set (a zero stays zero).
	if opts.TopN != nil {
		body["top_n"] = *opts.TopN
	}

	if po, ok := opts.ProviderOptions["cohere"].(map[string]interface{}); ok {
		if v, ok := po["maxTokensPerDoc"]; ok && v != nil {
			body["max_tokens_per_doc"] = v
		}
		if v, ok := po["priority"]; ok && v != nil {
			body["priority"] = v
		}
	}
	return body, warnings
}

// convertResponse converts a Cohere response to RerankResult
func (m *RerankingModel) convertResponse(response cohereRerankResponse) *types.RerankResult {
	result := &types.RerankResult{
		Ranking: make([]types.RerankItem, len(response.Results)),
		Response: types.RerankResponse{
			ID:        response.ID,
			Timestamp: time.Now(),
			ModelID:   m.modelID,
		},
	}

	// Convert ranking results
	for i, item := range response.Results {
		result.Ranking[i] = types.RerankItem{
			Index:          item.Index,
			RelevanceScore: item.RelevanceScore,
		}
	}

	return result
}

// handleError converts various errors to provider errors
func (m *RerankingModel) handleError(err error) error {
	return providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
}

// cohereRerankResponse represents the Cohere rerank API response
type cohereRerankResponse struct {
	ID      string `json:"id"`
	Results []struct {
		Index          int         `json:"index"`
		RelevanceScore float64     `json:"relevance_score"`
		Document       interface{} `json:"document,omitempty"`
	} `json:"results"`
	Meta *struct {
		APIVersion struct {
			Version string `json:"version"`
		} `json:"api_version"`
	} `json:"meta,omitempty"`
}

// cohereRerankingOptionsSchema mirrors cohereRerankingModelOptionsSchema (TS
// reranking/cohere-reranking-model-options.ts).
var cohereRerankingOptionsSchema = providerutils.OptionSchema{
	"maxTokensPerDoc": providerutils.OptNumber,
	"priority":        providerutils.OptNumber,
}
