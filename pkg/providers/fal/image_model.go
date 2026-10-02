package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "fal"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// falFileInputSupportedModels lists Fal model IDs known to accept file
// inputs for image-to-image editing (TS FalImageModel#supportsFileInputs).
var falFileInputSupportedModels = map[string]bool{
	"fal-ai/flux-2/edit":                 true,
	"fal-ai/flux-pro/kontext":            true,
	"fal-ai/flux-pro/kontext/max":        true,
	"fal-ai/flux-general/image-to-image": true,
	"fal-ai/flux-general/inpainting":     true,
	"fal-ai/flux-lora/image-to-image":    true,
	"fal-ai/flux-lora/inpainting":        true,
	"fal-ai/flux/dev/image-to-image":     true,
	"fal-ai/flux/krea/image-to-image":    true,
	"fal-ai/recraft/v3/image-to-image":   true,
}

// falFileInputUnsupportedModels lists Fal model IDs known to be text-to-image
// only, so they explicitly report no file-input support.
var falFileInputUnsupportedModels = map[string]bool{
	"bria/text-to-image/3.2":                       true,
	"fal-ai/bria/text-to-image/base":               true,
	"fal-ai/bria/text-to-image/fast":               true,
	"fal-ai/bria/text-to-image/hd":                 true,
	"fal-ai/bytedance/dreamina/v3.1/text-to-image": true,
	"fal-ai/flux-kontext-lora/text-to-image":       true,
	"fal-ai/recraft/v3/text-to-image":              true,
	"fal-ai/wan/v2.2-5b/text-to-image":             true,
	"fal-ai/wan/v2.2-a14b/text-to-image":           true,
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if falFileInputSupportedModels[m.modelID] {
		return boolPtr(true)
	}
	if falFileInputUnsupportedModels[m.modelID] {
		return boolPtr(false)
	}
	return nil
}

// SupportsMaskInputs reports whether the model accepts a mask input for
// inpainting. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if m.modelID == "fal-ai/flux-general/inpainting" || m.modelID == "fal-ai/flux-lora/inpainting" {
		return boolPtr(true)
	}
	if m.SupportsFileInputs() == nil {
		return nil
	}
	return boolPtr(false)
}

func boolPtr(b bool) *bool {
	return &b
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
		return nil, fmt.Errorf("fal.ai API returned status %d: %s", resp.StatusCode, string(resp.Body))
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
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height) //nolint:errcheck
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
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 30 * time.Second
	return fileutil.Download(ctx, url, opts)
}

type falImageResponse struct {
	Images []struct {
		URL         string `json:"url"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		ContentType string `json:"content_type"`
	} `json:"images"`
}
