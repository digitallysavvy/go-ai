package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// EmbeddingModel implements the provider.EmbeddingModel interface for Hugging Face
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Hugging Face embedding model
func NewEmbeddingModel(provider *Provider, modelID string) *EmbeddingModel {
	return &EmbeddingModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *EmbeddingModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *EmbeddingModel) Provider() string {
	return "huggingface"
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Hugging Face Inference API supports 1 embedding per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 1
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding generation for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	path := fmt.Sprintf("/models/%s", m.modelID)
	reqBody := map[string]interface{}{
		"inputs": input,
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: optsHeaders(opts),
	})
	if err != nil {
		return nil, providererrors.NewProviderError("huggingface", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("hugging face API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	embedding, err := m.parseEmbeddingResponse(resp.Body)
	if err != nil {
		return nil, err
	}

	// HF doesn't return token counts, approximate
	totalTokens := len(input) / 4

	return &types.EmbeddingResult{
		Embedding: embedding,
		Usage: types.EmbeddingUsage{
			InputTokens: totalTokens,
			TotalTokens: totalTokens,
		},
		Response: types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(resp.Headers)},
	}, nil
}

// DoEmbedMany performs embedding generation for multiple inputs
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	path := fmt.Sprintf("/models/%s", m.modelID)

	var embeddings [][]float64
	var totalTokens int
	responses := make([]types.EmbeddingResponse, 0, len(inputs))

	for _, input := range inputs {
		reqBody := map[string]interface{}{
			"inputs": input,
		}

		resp, err := m.provider.client.Do(ctx, internalhttp.Request{
			Method:  http.MethodPost,
			Path:    path,
			Body:    reqBody,
			Headers: optsHeaders(opts),
		})
		if err != nil {
			return nil, providererrors.NewProviderError("huggingface", 0, "", err.Error(), err)
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("hugging face API returned status %d: %s", resp.StatusCode, string(resp.Body))
		}

		embedding, err := m.parseEmbeddingResponse(resp.Body)
		if err != nil {
			return nil, err
		}

		embeddings = append(embeddings, embedding)
		totalTokens += len(input) / 4
		responses = append(responses, types.EmbeddingResponse{Headers: providerutils.ExtractHeaders(resp.Headers)})
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: totalTokens,
			TotalTokens: totalTokens,
		},
		Responses: responses,
	}, nil
}

func (m *EmbeddingModel) parseEmbeddingResponse(body []byte) ([]float64, error) {
	// Try parsing as direct array
	var embedding []float64
	if err := json.Unmarshal(body, &embedding); err == nil {
		return embedding, nil
	}

	// Try parsing as array of arrays (some models return this)
	var embeddings [][]float64
	if err := json.Unmarshal(body, &embeddings); err == nil && len(embeddings) > 0 {
		return embeddings[0], nil
	}

	// Try parsing as object with embedding field
	var objResp struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &objResp); err == nil && len(objResp.Embedding) > 0 {
		return objResp.Embedding, nil
	}

	// Try error format
	var errorResp hfErrorResponse
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != "" {
		return nil, fmt.Errorf("hugging face API error: %s", errorResp.Error)
	}

	return nil, fmt.Errorf("unexpected response format from Hugging Face: %s", string(body))
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}
