package together

import (
	"context"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// RerankingModel implements the provider.RerankingModel interface for
// Together AI. Ports packages/togetherai/src/reranking/togetherai-reranking-model.ts
// (ai@7.0.118): POST {baseURL}/rerank with {model, documents, query, top_n,
// rank_fields, return_documents:false}.
type RerankingModel struct {
	provider *Provider
	modelID  string
}

// NewRerankingModel creates a new Together AI reranking model.
func NewRerankingModel(provider *Provider, modelID string) *RerankingModel {
	return &RerankingModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *RerankingModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *RerankingModel) Provider() string { return "together" }

// ModelID returns the model ID.
func (m *RerankingModel) ModelID() string { return m.modelID }

// togetherRerankingOptions mirrors togetheraiRerankingModelOptionsSchema:
// TS's TogetherAIRerankingModelOptions.rankFields.
type togetherRerankingOptions struct {
	// RankFields lists keys in the JSON object document to rank by.
	// Defaults to using all supplied keys for ranking.
	RankFields []string `json:"rankFields,omitempty"`
}

func extractTogetherRerankingOptions(providerOptions map[string]interface{}) togetherRerankingOptions {
	po, ok := providerOptions["together"].(map[string]interface{})
	if !ok {
		return togetherRerankingOptions{}
	}
	var opts togetherRerankingOptions
	if raw, ok := po["rankFields"]; ok {
		if slice, ok := raw.([]interface{}); ok {
			for _, v := range slice {
				if s, ok := v.(string); ok {
					opts.RankFields = append(opts.RankFields, s)
				}
			}
		} else if strs, ok := raw.([]string); ok {
			opts.RankFields = strs
		}
	}
	return opts
}

// DoRerank reranks documents against a query, matching TS's doRerank.
func (m *RerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	if opts == nil {
		return nil, providererrors.NewProviderError("together", 0, "", "rerank options cannot be nil", nil)
	}

	rerankOpts := extractTogetherRerankingOptions(opts.ProviderOptions)

	reqBody := map[string]interface{}{
		"model":            m.modelID,
		"documents":        togetherRerankDocuments(opts.Documents),
		"query":            opts.Query,
		"return_documents": false, // reduce response size, matches TS
	}
	if opts.TopN != nil {
		reqBody["top_n"] = *opts.TopN
	}
	if len(rerankOpts.RankFields) > 0 {
		reqBody["rank_fields"] = rerankOpts.RankFields
	}

	var response togetherRerankingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/rerank",
		Body:    reqBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("together", 0, "", err.Error(), err)
	}

	ranking := make([]types.RerankItem, 0, len(response.Results))
	for _, r := range response.Results {
		ranking = append(ranking, types.RerankItem{Index: r.Index, RelevanceScore: r.RelevanceScore})
	}

	return &types.RerankResult{
		Ranking: ranking,
		Response: types.RerankResponse{
			ID:        response.ID,
			Timestamp: time.Now(),
			ModelID:   firstNonEmpty(response.Model, m.modelID),
			Headers:   map[string][]string(httpResp.Headers),
			Body:      response,
		},
	}, nil
}

// togetherRerankDocuments passes documents through as-is (strings or JSON
// objects), matching TS's `documents: documents.values` — Together's API
// accepts either `string[]` or `JSONObject[]` directly, unlike providers that
// require documents to be pre-stringified.
func togetherRerankDocuments(docs interface{}) interface{} {
	switch v := docs.(type) {
	case []string:
		return v
	case []map[string]interface{}:
		return v
	case nil:
		return []string{}
	default:
		return v
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type togetherRerankingResponse struct {
	ID      string `json:"id,omitempty"`
	Model   string `json:"model,omitempty"`
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}
