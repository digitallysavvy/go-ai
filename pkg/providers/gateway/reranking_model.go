package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
		"documents": opts.Documents,
		"query":     opts.Query,
	}
	if opts.TopN != nil {
		body["topN"] = *opts.TopN
	}
	if len(opts.ProviderOptions) > 0 {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := m.getModelConfigHeaders()
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)
	for k, v := range opts.Headers {
		headers[k] = v
	}

	var response gatewayRerankResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/reranking-model",
		Body:    body,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	return &types.RerankResult{
		Ranking:          response.Ranking,
		ProviderMetadata: response.ProviderMetadata,
		Response: types.RerankResponse{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string][]string(httpResp.Headers),
			Body:      response,
		},
	}, nil
}

func (m *RerankingModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-reranking-model-specification-version": "4",
		"ai-model-id": m.modelID,
	}
}

func (m *RerankingModel) handleError(err error) error {
	lm := &LanguageModel{provider: m.provider, modelID: m.modelID}
	return lm.handleError(err)
}

type gatewayRerankResponse struct {
	Ranking          []types.RerankItem `json:"ranking"`
	ProviderMetadata interface{}        `json:"providerMetadata,omitempty"`
}
