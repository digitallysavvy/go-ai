package cohere

import (
	"context"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	return "v3"
}

// Provider returns the provider name
func (m *RerankingModel) Provider() string {
	return "cohere"
}

// ModelID returns the model ID
func (m *RerankingModel) ModelID() string {
	return m.modelID
}

// DoRerank performs document reranking
func (m *RerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts)

	// Make API request
	var response cohereRerankResponse
	err := m.provider.client.PostJSON(ctx, "/rerank", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response
	return m.convertResponse(response), nil
}

// buildRequestBody builds the Cohere rerank API request body
func (m *RerankingModel) buildRequestBody(opts *provider.RerankOptions) map[string]interface{} {
	body := map[string]interface{}{
		"model": m.modelID,
		"query": opts.Query,
	}

	// Convert documents to the format Cohere expects
	switch docs := opts.Documents.(type) {
	case []string:
		body["documents"] = docs
	case []map[string]interface{}:
		// Cohere accepts structured documents
		body["documents"] = docs
	default:
		// Fallback: try to convert to []interface{}
		body["documents"] = opts.Documents
	}

	// Add topN if specified
	if opts.TopN != nil && *opts.TopN > 0 {
		body["top_n"] = *opts.TopN
	}

	return body
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
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
		Document       interface{} `json:"document,omitempty"`
	} `json:"results"`
	Meta *struct {
		APIVersion struct {
			Version string `json:"version"`
		} `json:"api_version"`
	} `json:"meta,omitempty"`
}
