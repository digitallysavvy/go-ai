package xai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for XAI
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new XAI image generation model
func NewImageModel(prov *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: prov,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "xai"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// XAIImageProviderOptions contains provider-specific options for XAI image generation
type XAIImageProviderOptions struct {
	// AspectRatio for image generation (e.g., "16:9", "1:1", "9:16")
	AspectRatio *string `json:"aspect_ratio,omitempty"`

	// OutputFormat specifies the image format (e.g., "png", "jpeg")
	OutputFormat *string `json:"output_format,omitempty"`

	// SyncMode controls synchronous vs asynchronous generation
	SyncMode *bool `json:"sync_mode,omitempty"`
}

// DoGenerate performs image generation or editing
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	warnings := []types.Warning{}

	// Extract provider options
	provOpts, err := extractImageProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	// Check for unsupported options
	warnings = append(warnings, m.checkUnsupportedOptions(opts)...)

	// Determine if this is editing or generation
	hasFiles := len(opts.Files) > 0
	endpoint := "/v1/images/generations"
	if hasFiles {
		endpoint = "/v1/images/edits"
	}

	// Build request body
	body := m.buildRequestBody(opts, provOpts, hasFiles)

	// Make API request
	var resp xaiImageResponse
	if err := m.provider.client.PostJSON(ctx, endpoint, body, &resp); err != nil {
		return nil, m.handleError(err)
	}

	// Check if we have at least one image
	if len(resp.Data) == 0 {
		return nil, providererrors.NewProviderError("xai", 0, "",
			"no images in response", nil)
	}

	// Download first image (Go ImageResult only supports single image)
	imageBytes, err := m.downloadImage(ctx, resp.Data[0].URL)
	if err != nil {
		return nil, providererrors.NewProviderError("xai", 0, "",
			fmt.Sprintf("failed to download image: %v", err), err)
	}

	// Build result with single image
	result := &types.ImageResult{
		Image:    imageBytes,
		MimeType: "image/png", // XAI typically returns PNG
		Usage: types.ImageUsage{
			ImageCount: len(resp.Data),
		},
		Warnings: warnings,
	}

	return result, nil
}

// buildRequestBody constructs the API request body
func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, provOpts *XAIImageProviderOptions, hasFiles bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":           m.modelID,
		"prompt":          opts.Prompt,
		"response_format": "url",
	}

	// Add N (number of images)
	n := 1
	if opts.N != nil && *opts.N > 0 {
		n = *opts.N
	}
	body["n"] = n

	// Add aspect ratio (prefer standard option over provider option)
	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	} else if provOpts.AspectRatio != nil {
		body["aspect_ratio"] = *provOpts.AspectRatio
	}

	// Add provider-specific options
	if provOpts.OutputFormat != nil {
		body["output_format"] = *provOpts.OutputFormat
	}

	if provOpts.SyncMode != nil {
		body["sync_mode"] = *provOpts.SyncMode
	}

	// Add source image for editing/variations
	if hasFiles && len(opts.Files) > 0 {
		imageURL := m.convertImageFileToDataURI(opts.Files[0])
		body["image"] = map[string]interface{}{
			"url":  imageURL,
			"type": "image_url",
		}
	}

	// Add mask for inpainting
	if opts.Mask != nil {
		maskURL := m.convertImageFileToDataURI(*opts.Mask)
		body["mask"] = map[string]interface{}{
			"url":  maskURL,
			"type": "image_url",
		}
	}

	return body
}

// convertImageFileToDataURI converts an ImageFile to a data URI
func (m *ImageModel) convertImageFileToDataURI(file provider.ImageFile) string {
	if file.Type == "url" {
		return file.URL
	}

	// Convert binary data to base64 data URL
	base64Data := base64.StdEncoding.EncodeToString(file.Data)
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}

	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64Data)
}

// checkUnsupportedOptions checks for unsupported options and generates warnings
func (m *ImageModel) checkUnsupportedOptions(opts *provider.ImageGenerateOptions) []types.Warning {
	warnings := []types.Warning{}

	if opts.Size != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-option",
			Message: "XAI image model does not support the 'size' option. Use 'aspectRatio' instead.",
		})
	}

	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-option",
			Message: "XAI image model does not support seed",
		})
	}

	hasFiles := len(opts.Files) > 0
	if opts.Mask != nil && !hasFiles {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-option",
			Message: "Mask is only supported with image editing (requires Files)",
		})
	}

	if hasFiles && len(opts.Files) > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "XAI only supports a single input image. Additional images are ignored.",
		})
	}

	return warnings
}

// downloadImage downloads an image from a URL with size limits to prevent DoS
func (m *ImageModel) downloadImage(ctx context.Context, url string) ([]byte, error) {
	return fileutil.Download(ctx, url, fileutil.DefaultDownloadOptions())
}

// extractImageProviderOptions extracts XAI-specific provider options
func extractImageProviderOptions(opts map[string]interface{}) (*XAIImageProviderOptions, error) {
	if opts == nil {
		return &XAIImageProviderOptions{}, nil
	}

	xaiOpts, ok := opts["xai"]
	if !ok {
		return &XAIImageProviderOptions{}, nil
	}

	// Convert to JSON and back to struct
	jsonData, err := json.Marshal(xaiOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal provider options: %w", err)
	}

	var provOpts XAIImageProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider options: %w", err)
	}

	return &provOpts, nil
}

// handleError converts provider errors
func (m *ImageModel) handleError(err error) error {
	if provErr, ok := err.(*providererrors.ProviderError); ok {
		return provErr
	}
	return providererrors.NewProviderError("xai", 0, "", err.Error(), err)
}

// xaiImageResponse represents the image generation API response
type xaiImageResponse struct {
	Data []xaiImageData `json:"data"`
}

// xaiImageData represents image data in the response
type xaiImageData struct {
	URL           string  `json:"url"`
	RevisedPrompt *string `json:"revised_prompt,omitempty"`
}
