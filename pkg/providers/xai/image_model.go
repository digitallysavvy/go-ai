package xai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
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
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "xai.image"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// MaxImagesPerCall returns the maximum number of images accepted in one request.
func (m *ImageModel) MaxImagesPerCall() int {
	return 3
}

// XAIImageProviderOptions contains provider-specific options for XAI image generation
type XAIImageProviderOptions struct {
	// AspectRatio for image generation (e.g., "16:9", "1:1", "9:16")
	AspectRatio *string `json:"aspect_ratio,omitempty"`

	// OutputFormat specifies the image format.
	// Valid values: "png", "jpeg", "b64_json".
	// Use "b64_json" to receive base64-encoded image data instead of a URL.
	OutputFormat *string `json:"output_format,omitempty"`

	// SyncMode controls synchronous vs asynchronous generation
	SyncMode *bool `json:"sync_mode,omitempty"`

	// Resolution controls the output resolution tier of the generated image.
	// Accepted values: "1k" (1024px), "2k" (2048px).
	// Only supported by models that accept this option (e.g., grok-imagine-image-pro).
	Resolution *string `json:"resolution,omitempty"`

	// Quality controls the output quality.
	// Valid values: "low", "medium", "high".
	Quality *string `json:"quality,omitempty"`

	// User is a unique identifier for the end user, used for abuse detection.
	User *string `json:"user,omitempty"`
}

// XAIImageMetadata holds provider-specific metadata returned by XAI image models.
type XAIImageMetadata struct {
	// Images contains per-image metadata (e.g., revised prompts).
	Images []XAIImageItemMetadata `json:"images,omitempty"`

	// CostInUsdTicks is the cost of the image generation in USD ticks
	// (1 tick = 0.000001 USD).
	CostInUsdTicks *int64 `json:"costInUsdTicks,omitempty"`
}

// XAIImageItemMetadata holds per-image metadata from the XAI image API.
type XAIImageItemMetadata struct {
	// RevisedPrompt is the prompt that was actually used to generate the image,
	// after any safety or quality revisions applied by the model.
	RevisedPrompt *string `json:"revisedPrompt,omitempty"`
}

// DoGenerate performs image generation or editing
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	callCtx, cancel := imageCallContext(ctx, opts.AbortSignal)
	if cancel != nil {
		defer cancel()
	}
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
	responseTimestamp := time.Now()

	// Make API request
	var resp xaiImageResponse
	httpResp, err := m.provider.client.Do(callCtx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    endpoint,
		Body:    body,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	if httpResp.StatusCode >= 400 {
		return nil, newXAIProviderError("xai.image", httpResp.StatusCode, httpResp.Body)
	}
	if err := json.Unmarshal(httpResp.Body, &resp); err != nil {
		return nil, providererrors.NewProviderError("xai.image", 0, "",
			fmt.Sprintf("failed to decode JSON response: %v", err), err)
	}

	// Check if we have at least one image
	if len(resp.Data) == 0 {
		return nil, providererrors.NewProviderError("xai.image", 0, "",
			"no images in response", nil)
	}

	images, base64Images, err := m.responseImages(callCtx, resp.Data)
	if err != nil {
		return nil, err
	}

	// Build result with single image
	result := &types.ImageResult{
		Image:    images[0],
		Images:   images,
		MimeType: "image/png",
		URL:      resp.Data[0].URL,
		Usage: types.ImageUsage{
			ImageCount: len(resp.Data),
		},
		Warnings: warnings,
		Response: &types.ResponseMetadata{
			Timestamp: responseTimestamp,
			ModelID:   m.modelID,
			Headers:   flattenHeaders(httpResp.Headers),
		},
	}
	if len(base64Images) > 0 {
		result.Base64Image = base64Images[0]
		result.Base64Images = base64Images
	}

	// Always build providerMetadata with per-image array (exposes revisedPrompt).
	imagesMeta := make([]XAIImageItemMetadata, len(resp.Data))
	for i, d := range resp.Data {
		imagesMeta[i] = XAIImageItemMetadata{RevisedPrompt: d.RevisedPrompt}
	}
	meta := XAIImageMetadata{Images: imagesMeta}
	if resp.Usage != nil && resp.Usage.CostInUsdTicks != nil {
		meta.CostInUsdTicks = resp.Usage.CostInUsdTicks
	}
	result.ProviderMetadata = map[string]interface{}{"xai": meta}

	return result, nil
}

func imageCallContext(ctx context.Context, abortSignal context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if abortSignal == nil {
		return ctx, nil
	}
	callCtx, cancel := context.WithCancel(ctx)
	select {
	case <-abortSignal.Done():
		cancel()
		return callCtx, cancel
	default:
	}
	stop := context.AfterFunc(abortSignal, cancel)
	return callCtx, func() {
		stop()
		cancel()
	}
}

func (m *ImageModel) responseImages(ctx context.Context, data []xaiImageData) ([][]byte, []string, error) {
	hasAllBase64 := true
	for _, image := range data {
		if image.B64JSON == "" {
			hasAllBase64 = false
			break
		}
	}

	images := make([][]byte, 0, len(data))
	if hasAllBase64 {
		base64Images := make([]string, 0, len(data))
		for _, image := range data {
			decoded, err := base64.StdEncoding.DecodeString(image.B64JSON)
			if err != nil {
				return nil, nil, providererrors.NewProviderError("xai.image", 0, "",
					fmt.Sprintf("failed to decode b64_json image: %v", err), err)
			}
			images = append(images, decoded)
			base64Images = append(base64Images, image.B64JSON)
		}
		return images, base64Images, nil
	}

	for _, image := range data {
		if image.URL == "" {
			return nil, nil, providererrors.NewProviderError("xai.image", 0, "",
				"no image data (neither url nor b64_json) in response", nil)
		}
		imageBytes, err := m.downloadImage(ctx, image.URL)
		if err != nil {
			return nil, nil, providererrors.NewProviderError("xai.image", 0, "",
				fmt.Sprintf("failed to download image: %v", err), err)
		}
		images = append(images, imageBytes)
	}
	return images, nil, nil
}

func flattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	flattened := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			flattened[key] = values[0]
		}
	}
	return flattened
}

// buildRequestBody constructs the API request body
func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, provOpts *XAIImageProviderOptions, hasFiles bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":           m.modelID,
		"prompt":          opts.Prompt,
		"response_format": "b64_json", // Always request base64 data directly from the API.
	}

	if opts.N != nil {
		body["n"] = *opts.N
	}

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

	if provOpts.Resolution != nil {
		body["resolution"] = *provOpts.Resolution
	}

	if provOpts.Quality != nil {
		body["quality"] = *provOpts.Quality
	}

	if provOpts.User != nil {
		body["user"] = *provOpts.User
	}

	// Add source images for editing.
	if hasFiles {
		if len(opts.Files) == 1 {
			body["image"] = map[string]interface{}{
				"url":  m.convertImageFileToDataURI(opts.Files[0]),
				"type": "image_url",
			}
		} else {
			images := make([]map[string]interface{}, 0, len(opts.Files))
			for _, f := range opts.Files {
				images = append(images, map[string]interface{}{
					"url":  m.convertImageFileToDataURI(f),
					"type": "image_url",
				})
			}
			body["images"] = images
		}
	}

	// mask is not supported by the xAI image API — omitted intentionally.

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
			Type:    "unsupported",
			Feature: "size",
			Details: "This model does not support the `size` option. Use `aspectRatio` instead.",
		})
	}

	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
		})
	}

	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "mask",
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
	if provOpts.Resolution != nil && *provOpts.Resolution != "1k" && *provOpts.Resolution != "2k" {
		return nil, fmt.Errorf("invalid xai image resolution %q: expected \"1k\" or \"2k\"", *provOpts.Resolution)
	}
	if provOpts.Quality != nil {
		switch *provOpts.Quality {
		case "low", "medium", "high":
		default:
			return nil, fmt.Errorf("invalid xai image quality %q: expected \"low\", \"medium\", or \"high\"", *provOpts.Quality)
		}
	}

	return &provOpts, nil
}

// handleError converts provider errors
func (m *ImageModel) handleError(err error) error {
	if provErr, ok := err.(*providererrors.ProviderError); ok {
		return provErr
	}
	return providererrors.NewProviderError("xai.image", 0, "", err.Error(), err)
}

// xaiImageResponse represents the image generation API response
type xaiImageResponse struct {
	Data  []xaiImageData `json:"data"`
	Usage *xaiImageUsage `json:"usage,omitempty"`
}

// xaiImageUsage holds top-level usage data from the image generation response.
type xaiImageUsage struct {
	CostInUsdTicks *int64 `json:"cost_in_usd_ticks,omitempty"`
}

// xaiImageData represents image data in the response
type xaiImageData struct {
	URL           string  `json:"url,omitempty"`
	B64JSON       string  `json:"b64_json,omitempty"`
	RevisedPrompt *string `json:"revised_prompt,omitempty"`
}
