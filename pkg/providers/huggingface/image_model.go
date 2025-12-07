package huggingface

import (
	"context"
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Hugging Face
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Hugging Face image generation model
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
	return "huggingface"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	path := fmt.Sprintf("/models/%s", m.modelID)
	resp, err := m.provider.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("huggingface", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Hugging Face API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return m.convertResponse(resp.Body)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"inputs": opts.Prompt,
	}

	parameters := make(map[string]interface{})

	if opts.N != nil && *opts.N > 1 {
		parameters["num_images"] = *opts.N
	}

	if opts.Size != "" {
		var width, height int
		fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			parameters["width"] = width
			parameters["height"] = height
		}
	}

	if len(parameters) > 0 {
		reqBody["parameters"] = parameters
	}

	return reqBody
}

func (m *ImageModel) convertResponse(body []byte) (*types.ImageResult, error) {
	// Hugging Face returns the image as raw bytes for image generation models
	// The response is the actual image data, not JSON

	// Check if it's an error response (JSON)
	var errorResp hfErrorResponse
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != "" {
		return nil, fmt.Errorf("Hugging Face API error: %s", errorResp.Error)
	}

	// If not an error, treat as image data
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from Hugging Face")
	}

	// Detect mime type from first few bytes
	mimeType := "image/png"
	if len(body) >= 2 {
		if body[0] == 0xFF && body[1] == 0xD8 {
			mimeType = "image/jpeg"
		} else if len(body) >= 4 && body[0] == 0x89 && body[1] == 0x50 && body[2] == 0x4E && body[3] == 0x47 {
			mimeType = "image/png"
		}
	}

	return &types.ImageResult{
		Image:    body,
		MimeType: mimeType,
		URL:      "",
		Usage:    types.ImageUsage{},
	}, nil
}
