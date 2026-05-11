package google

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// EmbeddingPart is implemented by TextEmbeddingPart and ImageEmbeddingPart.
// The unexported marker method seals the interface within this package.
type EmbeddingPart interface {
	embeddingPart()
}

// TextEmbeddingPart is a text part for multimodal embedding.
type TextEmbeddingPart struct {
	Text string
}

func (TextEmbeddingPart) embeddingPart() {}

// ImageEmbeddingPart is an image part for multimodal embedding.
type ImageEmbeddingPart struct {
	// MimeType is the MIME type of the image (e.g., "image/jpeg").
	MimeType string
	// Data is the raw image bytes.
	Data []byte
}

func (ImageEmbeddingPart) embeddingPart() {}

// GoogleEmbeddingProviderOptions contains Google-specific options for embedding.
type GoogleEmbeddingProviderOptions struct {
	// Parts provides additional multimodal content parts alongside the text input.
	// Each element corresponds to a single embedding value and is appended to that
	// value's content.parts in the API request.
	Parts []EmbeddingPart
}

// EmbeddingModel implements the provider.EmbeddingModel interface for Google
type EmbeddingModel struct {
	provider *Provider
	modelID  string
}

// NewEmbeddingModel creates a new Google embedding model
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
	return m.provider.Name()
}

// ModelID returns the model ID
func (m *EmbeddingModel) ModelID() string {
	return m.modelID
}

// MaxEmbeddingsPerCall returns the maximum number of embeddings per call
// Google supports 100 embeddings per API call
func (m *EmbeddingModel) MaxEmbeddingsPerCall() int {
	return 100
}

// SupportsParallelCalls returns whether parallel calls are supported
func (m *EmbeddingModel) SupportsParallelCalls() bool {
	return true
}

// DoEmbed performs embedding for a single input
func (m *EmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	reqBody := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", m.modelID),
		"content": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": input},
			},
		},
	}
	path := fmt.Sprintf("/models/%s:embedContent", m.modelID)

	var response googleEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: optsHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if response.Embedding == nil || len(response.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return &types.EmbeddingResult{
		Embedding: response.Embedding.Values,
		Usage: types.EmbeddingUsage{
			// Google doesn't return token counts for embeddings
			InputTokens: 0,
			TotalTokens: 0,
		},
		Response: types.EmbeddingResponse{Headers: map[string][]string(httpResp.Headers)},
	}, nil
}

// DoEmbedMany performs embedding for multiple inputs in a batch
func (m *EmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) == 0 {
		return &types.EmbeddingsResult{
			Embeddings: [][]float64{},
			Usage:      types.EmbeddingUsage{},
		}, nil
	}

	// Google doesn't have a native batch API, so we call individually.
	embeddings := make([][]float64, len(inputs))
	responses := make([]types.EmbeddingResponse, 0, len(inputs))
	for i, input := range inputs {
		result, err := m.DoEmbed(ctx, input, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to embed input %d: %w", i, err)
		}
		embeddings[i] = result.Embedding
		responses = append(responses, result.Response)
	}

	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage: types.EmbeddingUsage{
			InputTokens: 0,
			TotalTokens: 0,
		},
		Responses: responses,
	}, nil
}

// DoEmbedParts performs embedding for a single input with optional multimodal parts.
// The text parameter provides the primary text content; parts provides additional
// content (e.g. an image) to embed alongside it.
func (m *EmbeddingModel) DoEmbedParts(ctx context.Context, text string, parts []EmbeddingPart) (*types.EmbeddingResult, error) {
	apiParts := buildEmbeddingAPIParts(text, parts)

	reqBody := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", m.modelID),
		"content": map[string]interface{}{
			"parts": apiParts,
		},
	}
	path := fmt.Sprintf("/models/%s:embedContent", m.modelID)

	var response googleEmbeddingResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if response.Embedding == nil || len(response.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return &types.EmbeddingResult{
		Embedding: response.Embedding.Values,
		Usage:     types.EmbeddingUsage{},
		Response:  types.EmbeddingResponse{Headers: map[string][]string(httpResp.Headers)},
	}, nil
}

// buildEmbeddingAPIParts converts a text string plus optional EmbeddingParts to the
// list of content.parts expected by the Google embedContent API.
func buildEmbeddingAPIParts(text string, parts []EmbeddingPart) []map[string]interface{} {
	apiParts := []map[string]interface{}{
		{"text": text},
	}
	for _, p := range parts {
		switch v := p.(type) {
		case TextEmbeddingPart:
			apiParts = append(apiParts, map[string]interface{}{"text": v.Text})
		case ImageEmbeddingPart:
			apiParts = append(apiParts, map[string]interface{}{
				"inlineData": map[string]interface{}{
					"mimeType": v.MimeType,
					"data":     base64.StdEncoding.EncodeToString(v.Data),
				},
			})
		}
	}
	return apiParts
}

// handleError converts various errors to provider errors
func (m *EmbeddingModel) handleError(err error) error {
	return providererrors.NewProviderError("google", 0, "", err.Error(), err)
}

// optsHeaders extracts the Headers map from EmbedModelOptions (nil-safe).
func optsHeaders(opts *provider.EmbedModelOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

// googleEmbeddingResponse represents the Google embeddings API response
type googleEmbeddingResponse struct {
	Embedding *struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}
