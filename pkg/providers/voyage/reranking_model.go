package voyage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type RerankingModel struct {
	provider *Provider
	modelID  string
}

func NewRerankingModel(provider *Provider, modelID string) *RerankingModel {
	return &RerankingModel{provider: provider, modelID: modelID}
}

func (m *RerankingModel) SpecificationVersion() string { return "v4" }
func (m *RerankingModel) Provider() string             { return "voyage.reranking" }
func (m *RerankingModel) ModelID() string              { return m.modelID }

func (m *RerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("rerank options cannot be nil")
	}
	docs, warnings := toVoyageDocuments(opts.Documents)
	reqBody := map[string]interface{}{
		"query":     opts.Query,
		"documents": docs,
		"model":     m.modelID,
	}
	if opts.TopN != nil {
		reqBody["top_k"] = *opts.TopN
	}
	if po, ok := opts.ProviderOptions["voyage"].(map[string]interface{}); ok {
		if v, ok := po["returnDocuments"]; ok {
			reqBody["return_documents"] = v
		}
		if v, ok := po["truncation"]; ok {
			reqBody["truncation"] = v
		}
	}

	var response struct {
		Data []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"data"`
	}
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/rerank",
		Body:    reqBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("voyage", 0, "", err.Error(), err)
	}

	ranking := make([]types.RerankItem, 0, len(response.Data))
	for _, item := range response.Data {
		ranking = append(ranking, types.RerankItem{Index: item.Index, RelevanceScore: item.RelevanceScore})
	}
	return &types.RerankResult{
		Ranking:  ranking,
		Warnings: warnings,
		Response: types.RerankResponse{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string][]string(httpResp.Headers),
			Body:      response,
		},
	}, nil
}

func toVoyageDocuments(docs interface{}) ([]string, []types.Warning) {
	switch v := docs.(type) {
	case []string:
		return v, nil
	case []map[string]interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			raw, _ := json.Marshal(item)
			out = append(out, string(raw))
		}
		return out, []types.Warning{{
			Type:    "compatibility",
			Message: "Object documents are converted to strings.",
		}}
	default:
		return []string{}, nil
	}
}
