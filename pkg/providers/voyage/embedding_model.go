package voyage

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

func NewEmbeddingModel(provider *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{provider: provider, modelID: modelID}
}

func (m *EmbeddingModel) SpecificationVersion() string { return "v4" }
func (m *EmbeddingModel) Provider() string             { return "voyage.embedding" }
func (m *EmbeddingModel) ModelID() string              { return m.modelID }
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int    { return 128 }
func (m *EmbeddingModel) SupportsParallelCalls() bool  { return true }

func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	out, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	res := &types.EmbeddingResult{Embedding: out.Embeddings[0], Usage: out.Usage, Warnings: out.Warnings}
	if len(out.Responses) > 0 {
		res.Response = out.Responses[0]
	}
	return res, nil
}

func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) > m.MaxEmbeddingsPerCall() {
		return nil, fmt.Errorf("too many embedding values for %s/%s: max %d, got %d", m.Provider(), m.modelID, m.MaxEmbeddingsPerCall(), len(inputs))
	}

	reqBody := map[string]interface{}{"input": inputs, "model": m.modelID}
	if voyageOpts := voyageProviderOptions(opts); voyageOpts != nil {
		if v, ok := voyageOpts["inputType"]; ok {
			reqBody["input_type"] = v
		}
		if v, ok := voyageOpts["truncation"]; ok {
			reqBody["truncation"] = v
		}
		if v, ok := voyageOpts["outputDimension"]; ok {
			reqBody["output_dimension"] = v
		}
		if v, ok := voyageOpts["outputDtype"]; ok {
			reqBody["output_dtype"] = v
		}
	}

	var response struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Usage *struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/embeddings",
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("voyage", 0, "", err.Error(), err)
	}
	sort.Slice(response.Data, func(i, j int) bool { return response.Data[i].Index < response.Data[j].Index })
	embeddings := make([][]float64, 0, len(response.Data))
	for _, item := range response.Data {
		embeddings = append(embeddings, item.Embedding)
	}
	tokens := 0
	if response.Usage != nil {
		tokens = response.Usage.TotalTokens
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage:      types.EmbeddingUsage{InputTokens: tokens, TotalTokens: tokens},
		Responses:  []types.EmbeddingResponse{{Headers: map[string][]string(httpResp.Headers), Body: response}},
	}, nil
}

func voyageProviderOptions(opts *provider.EmbedModelOptions) map[string]interface{} {
	if opts == nil || opts.ProviderOptions == nil {
		return nil
	}
	raw, ok := opts.ProviderOptions["voyage"]
	if !ok {
		return nil
	}
	asMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	return asMap
}
