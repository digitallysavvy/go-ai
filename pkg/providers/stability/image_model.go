package stability

import (
	"context"
	"encoding/base64"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Stability AI
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Stability AI image generation model
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
	return "stability"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	path := fmt.Sprintf("/v1/generation/%s/text-to-image", m.modelID)
	var response stabilityImageResponse
	err := m.provider.client.PostJSON(ctx, path, reqBody, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("stability", 0, "", err.Error(), err)
	}

	return m.convertResponse(response)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"text_prompts": []map[string]interface{}{
			{
				"text":   opts.Prompt,
				"weight": 1,
			},
		},
	}

	if opts.Size != "" {
		// Parse size like "1024x1024"
		body["width"] = 1024
		body["height"] = 1024
	}

	if opts.N != nil {
		body["samples"] = *opts.N
	} else {
		body["samples"] = 1
	}

	return body
}

func (m *ImageModel) convertResponse(response stabilityImageResponse) (*types.ImageResult, error) {
	if len(response.Artifacts) == 0 {
		return nil, fmt.Errorf("no image data returned from Stability AI")
	}

	artifact := response.Artifacts[0]
	imageBytes, err := base64.StdEncoding.DecodeString(artifact.Base64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 image: %w", err)
	}

	return &types.ImageResult{
		Image:    imageBytes,
		MimeType: "image/png",
		Usage: types.ImageUsage{
			ImageCount: len(response.Artifacts),
		},
	}, nil
}

type stabilityImageResponse struct {
	Artifacts []struct {
		Base64       string `json:"base64"`
		FinishReason string `json:"finishReason"`
		Seed         int    `json:"seed"`
	} `json:"artifacts"`
}
