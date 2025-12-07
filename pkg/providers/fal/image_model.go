package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Fal.ai
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Fal.ai image generation model
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
	return "fal"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	path := fmt.Sprintf("/%s", m.modelID)
	resp, err := m.provider.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("fal", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Fal.ai API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var response falImageResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return m.convertResponse(ctx, response)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	if opts.Size != "" {
		var width, height int
		fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 {
			body["image_size"] = map[string]interface{}{
				"width":  width,
				"height": height,
			}
		}
	}

	if opts.N != nil {
		body["num_images"] = *opts.N
	}

	return body
}

func (m *ImageModel) convertResponse(ctx context.Context, response falImageResponse) (*types.ImageResult, error) {
	if len(response.Images) == 0 {
		return nil, fmt.Errorf("no images generated")
	}

	// Fal returns URLs, download the first image
	imageURL := response.Images[0].URL
	imageData, err := m.downloadImage(ctx, imageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}

	return &types.ImageResult{
		Image:    imageData,
		MimeType: response.Images[0].ContentType,
		URL:      imageURL,
		Usage:    types.ImageUsage{ImageCount: len(response.Images)},
	}, nil
}

func (m *ImageModel) downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type falImageResponse struct {
	Images []struct {
		URL         string `json:"url"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		ContentType string `json:"content_type"`
	} `json:"images"`
}
