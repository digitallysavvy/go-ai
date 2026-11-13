package gateway

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// RerankingModel implements provider.RerankingModel for AI Gateway.
type RerankingModel struct {
	provider *Provider
	modelID  string
}

// NewRerankingModel creates a new Gateway reranking model.
func NewRerankingModel(provider *Provider, modelID string) *RerankingModel {
	return &RerankingModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *RerankingModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name.
func (m *RerankingModel) Provider() string {
	return "gateway"
}

// ModelID returns the model ID.
func (m *RerankingModel) ModelID() string {
	return m.modelID
}

// DoRerank reranks documents through the AI Gateway reranking endpoint.
func (m *RerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("rerank options cannot be nil")
	}
	body := map[string]interface{}{
		"documents": gatewayRerankDocuments(opts.Documents),
		"query":     opts.Query,
	}
	if opts.TopN != nil {
		body["topN"] = *opts.TopN
	}
	if opts.ProviderOptions != nil {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := gatewayRequestHeaders(ctx, opts.Headers, m.getModelConfigHeaders())

	var response gatewayRerankResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator("gateway", providerutils.SchemaGatewayReranking),
		Method:           http.MethodPost,
		Path:             "/reranking-model",
		Body:             body,
		Headers:          headers,
	}, &response)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	return &types.RerankResult{
		Ranking:          response.ranking(),
		Warnings:         warningsOrEmpty(response.Warnings),
		ProviderMetadata: response.ProviderMetadata,
		Response: types.RerankResponse{
			Headers: map[string][]string(httpResp.Headers),
			Body:    rawJSONBody(httpResp.Body),
		},
	}, nil
}

func (m *RerankingModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-reranking-model-specification-version": "4",
		"ai-model-id": m.modelID,
	}
}

func (m *RerankingModel) handleErrorWithContext(ctx context.Context, err error) error {
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleErrorWithContext(ctx, err)
}

type gatewayRerankResponse struct {
	Ranking          []gatewayRerankItem `json:"ranking"`
	Warnings         []types.Warning     `json:"warnings,omitempty"`
	ProviderMetadata interface{}         `json:"providerMetadata,omitempty"`
}

// gatewayRerankDocuments wraps documents in the V4 reranking documents union
// that TS core rerank sends: {type: 'text', values} when the first document
// is a string, else {type: 'object', values} (rerank.ts documentsToSend).
// Any slice is accepted (strings, maps, json.RawMessage, structs).
func gatewayRerankDocuments(docs interface{}) interface{} {
	rv := reflect.ValueOf(docs)
	if docs == nil || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return docs
	}
	kind := "object"
	if rv.Len() > 0 {
		if _, ok := rv.Index(0).Interface().(string); ok {
			kind = "text"
		}
	}
	return map[string]interface{}{"type": kind, "values": docs}
}

// gatewayRerankItem is one gatewayRerankingResponseSchema ranking entry
// (index, relevanceScore); types.RerankItem has no json tags (v0.5.2).
type gatewayRerankItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevanceScore"`
}

func (r gatewayRerankResponse) ranking() []types.RerankItem {
	if r.Ranking == nil {
		return nil
	}
	out := make([]types.RerankItem, len(r.Ranking))
	for i, item := range r.Ranking {
		out[i] = types.RerankItem{Index: item.Index, RelevanceScore: item.RelevanceScore}
	}
	return out
}
