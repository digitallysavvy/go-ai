package fireworks

import (
	"context"
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Fireworks AI
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Fireworks AI image generation model
func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "fireworks"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	resp, err := m.provider.client.Post(ctx, "/v1/images/generations", reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("fireworks", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Fireworks AI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return m.convertResponse(resp.Body)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	if opts.N != nil {
		reqBody["n"] = *opts.N
	}

	if opts.Size != "" {
		var width, height int
		fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			reqBody["width"] = width
			reqBody["height"] = height
		}
	}

	return reqBody
}

func (m *ImageModel) convertResponse(body []byte) (*types.ImageResult, error) {
	var response struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no images generated")
	}

	result := &types.ImageResult{
		Usage: types.ImageUsage{},
	}

	if response.Data[0].URL != "" {
		result.URL = response.Data[0].URL
		result.MimeType = "image/png"
	}

	if response.Data[0].B64JSON != "" {
		result.Image = []byte(response.Data[0].B64JSON)
		result.MimeType = "image/png"
	}

	return result, nil
}
