package xai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

// xaiFileInputImageModelIDs are the image models that accept reference
// images, mirroring TS fileInputModelIds.
var xaiFileInputImageModelIDs = map[string]bool{
	ModelGrokImagineImage:    true,
	ModelGrokImagineImage20:  true,
	ModelGrokImagineImagePro: true,
}

// SupportsFileInputs reports true for models that accept reference images and
// nil (unknown) for any other model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if xaiFileInputImageModelIDs[m.modelID] {
		v := true
		return &v
	}
	return nil
}

// SupportsMaskInputs reports false for models that accept reference images
// (masks are not supported) and nil (unknown) otherwise.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if xaiFileInputImageModelIDs[m.modelID] {
		v := false
		return &v
	}
	return nil
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
	// Accepted values: "1k" (1024px), "1.5k", "2k" (2048px).
	// Only supported by models that accept this option (e.g., grok-imagine-image-pro).
	Resolution *string `json:"resolution,omitempty"`

	// Quality controls the output quality.
	// Valid values: "low", "medium", "high", "auto".
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
	endpoint := "/images/generations"
	if hasFiles {
		endpoint = "/images/edits"
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
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 { // TS !response.ok
		return nil, newXAIProviderErrorFrom("xai.image", httpResp.StatusError(httpResp.Body))
	}
	httpResp.Body = []byte(textdecode.UTF8(httpResp.Body)) // TS response.text(): BOM dropped, invalid UTF-8 replaced
	if err := providerutils.MediaResponseValidator("xai.image", providerutils.SchemaXAIImage)(httpResp.StatusCode, httpResp.Headers, httpResp.Body); err != nil {
		return nil, err
	}
	if err := providerutils.UnmarshalChunk(httpResp.Body, &resp); err != nil {
		return nil, providererrors.NewProviderError("xai.image", 0, "",
			fmt.Sprintf("failed to decode JSON response: %v", err), err)
	}

	// If any generated image was blocked by content moderation, the xAI API
	// still returns a 2xx response but flags the image with
	// respect_moderation: false. Surface this as an error, matching the TS
	// SDK's xai-image-model.ts behavior.
	for _, d := range resp.Data {
		if d.RespectModeration != nil && !*d.RespectModeration {
			// TS throws a plain Error (xai-image-model.ts:185-189).
			return nil, errors.New("Image generation was blocked due to a content policy violation.") //nolint:staticcheck // TS message text
		}
	}

	// TS returns an empty `images` array for an empty `data` array (core then
	// retries and throws NoImageGeneratedError), so this is not an error.
	if len(resp.Data) == 0 {
		meta := XAIImageMetadata{Images: []XAIImageItemMetadata{}}
		if resp.Usage != nil && resp.Usage.CostInUsdTicks != nil {
			meta.CostInUsdTicks = resp.Usage.CostInUsdTicks
		}
		return &types.ImageResult{
			MimeType: "image/png",
			Warnings: warnings,
			Response: &types.ResponseMetadata{
				Timestamp: responseTimestamp,
				ModelID:   m.modelID,
				Headers:   flattenHeaders(httpResp.Headers),
			},
			ProviderMetadata: map[string]interface{}{"xai": meta},
		}, nil
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
		// TS `...(item.revised_prompt ? { revisedPrompt } : {})`: an empty
		// revised prompt is left out.
		if d.RevisedPrompt != nil && *d.RevisedPrompt != "" {
			imagesMeta[i] = XAIImageItemMetadata{RevisedPrompt: d.RevisedPrompt}
		}
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
		// TS image.b64_json != null: an empty string is base64 data.
		if image.B64JSON == nil {
			hasAllBase64 = false
			break
		}
	}

	images := make([][]byte, 0, len(data))
	if hasAllBase64 {
		base64Images := make([]string, 0, len(data))
		for _, image := range data {
			decoded, err := fileutil.ConvertBase64ToBytes(*image.B64JSON)
			if err != nil {
				return nil, nil, providererrors.NewProviderError("xai.image", 0, "",
					fmt.Sprintf("failed to decode b64_json image: %v", err), err)
			}
			images = append(images, decoded)
			base64Images = append(base64Images, *image.B64JSON)
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
			// TS getFromApi with createStatusCodeErrorResponseHandler: an
			// HTTP failure is an APICallError with the status text; any
			// other failure (URL validation, abort) propagates unchanged,
			// and a network failure is the retryable "Cannot connect to
			// API" error (handleFetchError).
			if cause, ok := fileutil.TransportFailure(err); ok {
				return nil, nil, internalhttp.TransportErrorCtx(ctx, cause)
			}
			return nil, nil, providerutils.StatusCodeDownloadError("xai.image", err)
		}
		images = append(images, imageBytes)
	}
	return images, nil, nil
}

// flattenHeaders is TS extractResponseHeaders (repeated values joined with
// ", ", each byte decoded as one character): providerutils.ExtractHeaders.
// It kept only the first value of a repeated header.
func flattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	return providerutils.ExtractHeaders(headers)
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
	// url comes from the response body: validated and DNS-pinned, with the
	// configured base URL origin exempt (TS getFromApi validateUrl +
	// trustedOrigin: baseURL).
	return fileutil.Download(ctx, url, fileutil.TrustedOriginDownloadOptions(m.provider.config.BaseURL, m.provider.client.HTTPClient().Transport))
}

// extractImageProviderOptions extracts XAI-specific provider options
func extractImageProviderOptions(opts map[string]interface{}) (*XAIImageProviderOptions, error) {
	if opts == nil {
		return &XAIImageProviderOptions{}, nil
	}

	xaiOpts, ok := opts["xai"]
	if !ok || xaiOpts == nil {
		return &XAIImageProviderOptions{}, nil
	}

	// TS parseProviderOptions: a schema violation is an InvalidArgumentError
	// ("invalid xai provider options").
	invalid := func(cause error) error {
		return providererrors.NewArgumentError("providerOptions", "invalid xai provider options", cause)
	}

	// Convert to JSON and back to struct
	jsonData, err := json.Marshal(xaiOpts)
	if err != nil {
		return nil, invalid(fmt.Errorf("failed to marshal provider options: %w", err))
	}

	var provOpts XAIImageProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, invalid(fmt.Errorf("failed to unmarshal provider options: %w", err))
	}
	// TS xaiImageModelOptions members are .optional() (not nullish): a
	// present null is invalid.
	var probe map[string]json.RawMessage
	if json.Unmarshal(jsonData, &probe) == nil {
		for _, key := range []string{"aspect_ratio", "output_format", "sync_mode", "resolution", "quality", "user"} {
			if v, present := probe[key]; present && string(v) == "null" {
				return nil, invalid(fmt.Errorf("%s must not be null", key))
			}
		}
	}
	if provOpts.Resolution != nil && *provOpts.Resolution != "1k" && *provOpts.Resolution != "1.5k" && *provOpts.Resolution != "2k" {
		return nil, invalid(fmt.Errorf("invalid xai image resolution %q: expected \"1k\", \"1.5k\", or \"2k\"", *provOpts.Resolution))
	}
	if provOpts.Quality != nil {
		switch *provOpts.Quality {
		case "low", "medium", "high", "auto":
		default:
			return nil, invalid(fmt.Errorf("invalid xai image quality %q: expected \"low\", \"medium\", \"high\", or \"auto\"", *provOpts.Quality))
		}
	}

	return &provOpts, nil
}

// handleError converts provider errors
func (m *ImageModel) handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if provErr, ok := err.(*providererrors.ProviderError); ok {
		return provErr
	}
	if provErr, ok := xaiStatusError("xai.image", err); ok {
		return provErr // TS xaiFailedResponseHandler
	}
	return err // TS handleFetchError: no HTTP response, returned unchanged
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
	URL               string  `json:"url,omitempty"`
	B64JSON           *string `json:"b64_json,omitempty"`
	RevisedPrompt     *string `json:"revised_prompt,omitempty"`
	RespectModeration *bool   `json:"respect_moderation,omitempty"`
}
