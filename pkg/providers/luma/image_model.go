package luma

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Default polling configuration (TS LumaImageModel.pollIntervalMillis /
// maxPollAttempts): 500ms interval, 120 attempts (60s total).
const (
	defaultPollIntervalMillis = 500
	defaultMaxPollAttempts    = 60000 / defaultPollIntervalMillis
)

// ImageModel implements the provider.ImageModel interface for Luma AI.
// Luma exposes an asynchronous image generation queue: DoGenerate submits a
// generation, polls its status, and downloads the resulting image.
type ImageModel struct {
	prov    *Provider
	modelID string
}

// NewImageModel creates a new Luma image generation model.
func NewImageModel(prov *Provider, modelID string) *ImageModel {
	return &ImageModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *ImageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *ImageModel) Provider() string { return "luma.image" }

// ModelID returns the model ID.
func (m *ImageModel) ModelID() string { return m.modelID }

// MaxImagesPerCall returns 1 (Luma generates a single image per call,
// matching TS LumaImageModel's `readonly maxImagesPerCall = 1`).
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// DoGenerate submits a generation request, polls until it completes, and
// downloads the resulting image (TS LumaImageModel#doGenerate).
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}

	var warnings []types.Warning
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type: "unsupported", Feature: "seed",
			Details: "This model does not support the `seed` option.",
		})
	}
	if opts.Size != "" {
		warnings = append(warnings, types.Warning{
			Type: "unsupported", Feature: "size",
			Details: "This model does not support the `size` option. Use `aspectRatio` instead.",
		})
	}

	lumaOpts := parseImageModelOptions(opts.ProviderOptions)

	editingOptions, err := m.getEditingOptions(opts.Files, opts.Mask, lumaOpts)
	if err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"prompt": opts.Prompt,
		"model":  m.modelID,
	}
	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}
	for k, v := range editingOptions {
		body[k] = v
	}
	if lumaOpts != nil {
		for k, v := range lumaOpts.Additional {
			body[k] = v
		}
	}

	currentDate := time.Now()
	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/dream-machine/v1/generations/image",
		Body:    body,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, fmt.Errorf("luma: failed to submit generation request: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, m.parseErrorResponse(resp.Body, resp.StatusCode)
	}

	var genResp lumaGenerationResponse
	if err := json.Unmarshal(resp.Body, &genResp); err != nil {
		return nil, fmt.Errorf("luma: failed to decode generation response: %w", err)
	}

	pollIntervalMillis := defaultPollIntervalMillis
	maxPollAttempts := defaultMaxPollAttempts
	if lumaOpts != nil {
		if lumaOpts.PollIntervalMillis != nil {
			pollIntervalMillis = *lumaOpts.PollIntervalMillis
		}
		if lumaOpts.MaxPollAttempts != nil {
			maxPollAttempts = *lumaOpts.MaxPollAttempts
		}
	}

	imageURL, err := m.pollForImageURL(ctx, genResp.ID, opts.Headers, pollIntervalMillis, maxPollAttempts)
	if err != nil {
		return nil, err
	}

	imageData, contentType, err := m.downloadImage(ctx, imageURL)
	if err != nil {
		return nil, fmt.Errorf("luma: failed to download generated image: %w", err)
	}

	mimeType := contentType
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = media.DetectImageMediaType(imageData)
	}
	if mimeType == "" {
		mimeType = "image/png"
	}

	return &types.ImageResult{
		Image:    imageData,
		Images:   [][]byte{imageData},
		MimeType: mimeType,
		URL:      imageURL,
		Usage:    types.ImageUsage{},
		Warnings: warnings,
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   flattenHeaders(resp.Headers),
		},
	}, nil
}

// pollForImageURL polls the generation's status endpoint until it completes,
// fails, or maxPollAttempts is exhausted (TS LumaImageModel#pollForImageUrl).
// The status URL is built from the provider's own base URL and an opaque
// generation id (never a full URL taken from a response), matching TS's
// `getFromApi({ validateUrl: false })` for this call, so no additional SSRF
// validation is required here -- only the final image download (below) needs
// it, since that URL comes from the response body.
func (m *ImageModel) pollForImageURL(ctx context.Context, generationID string, headers map[string]string, pollIntervalMillis, maxPollAttempts int) (string, error) {
	path := "/dream-machine/v1/generations/" + generationID

	for i := 0; i < maxPollAttempts; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		resp, err := m.prov.client.Do(ctx, internalhttp.Request{
			Method:  "GET",
			Path:    path,
			Headers: headers,
		})
		if err != nil {
			return "", fmt.Errorf("luma: failed to check generation status: %w", err)
		}
		if resp.StatusCode >= 400 {
			return "", m.parseErrorResponse(resp.Body, resp.StatusCode)
		}

		var status lumaGenerationResponse
		if err := json.Unmarshal(resp.Body, &status); err != nil {
			return "", fmt.Errorf("luma: failed to parse status response: %w", err)
		}

		switch status.State {
		case "completed":
			if status.Assets == nil || status.Assets.Image == "" {
				return "", providererrors.NewInvalidResponseDataError(status, "Image generation completed but no image was found.")
			}
			return status.Assets.Image, nil
		case "failed":
			return "", providererrors.NewInvalidResponseDataError(status, "Image generation failed.")
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(pollIntervalMillis) * time.Millisecond):
		}
	}

	// TS reports the class-level default maxPollAttempts in this message
	// even when a call overrode it for the loop itself; matched here for
	// fidelity.
	return "", fmt.Errorf("Image generation timed out after %d attempts.", defaultMaxPollAttempts)
}

// downloadImage downloads the generated image. imageUrl is provider-response
// data (the generation's assets.image URL), so it is validated and
// DNS-pinned exactly like every other response-supplied download URL in this
// SDK (TS getFromApi({ validateUrl: true, trustedOrigin: baseURL })).
func (m *ImageModel) downloadImage(ctx context.Context, imageURL string) ([]byte, string, error) {
	opts := fileutil.TrustedOriginDownloadOptions(m.prov.baseURL(), nil)
	result, err := fileutil.DownloadWithMetadata(ctx, imageURL, opts)
	if err != nil {
		return nil, "", err
	}
	return result.Data, result.ContentType, nil
}

// getEditingOptions builds the reference-image request fields from Files and
// Mask, routed by ReferenceType (TS LumaImageModel#getEditingOptions). Luma
// has no mask-based inpainting and only accepts URL-based images.
func (m *ImageModel) getEditingOptions(files []provider.ImageFile, mask *provider.ImageFile, lumaOpts *ImageModelOptions) (map[string]interface{}, error) {
	options := map[string]interface{}{}

	if mask != nil {
		return nil, fmt.Errorf("Luma AI does not support mask-based image editing. " +
			"Use the prompt to describe the changes you want to make, along with " +
			"`prompt.images` containing the source image URL.")
	}

	if len(files) == 0 {
		return options, nil
	}

	for _, f := range files {
		if f.Type != "url" {
			return nil, fmt.Errorf("Luma AI only supports URL-based images. " +
				"Please provide image URLs using `prompt.images` with publicly accessible URLs. " +
				"Base64 and Uint8Array data are not supported.")
		}
	}

	referenceType := ReferenceTypeImage
	if lumaOpts != nil && lumaOpts.ReferenceType != nil {
		referenceType = *lumaOpts.ReferenceType
	}

	defaultWeights := map[string]float64{
		"image": 0.85, "style": 0.8, "character": 1.0, "modify_image": 1.0,
	}

	switch referenceType {
	case ReferenceTypeImage:
		if len(files) > 4 {
			return nil, fmt.Errorf("Luma AI image supports up to 4 reference images. You provided %d images.", len(files))
		}
		images := make([]map[string]interface{}, 0, len(files))
		for i, f := range files {
			weight := defaultWeights["image"]
			if cfg := lumaOpts.imageConfig(i); cfg.Weight != nil {
				weight = *cfg.Weight
			}
			images = append(images, map[string]interface{}{"url": f.URL, "weight": weight})
		}
		options["image"] = images

	case ReferenceTypeStyle:
		images := make([]map[string]interface{}, 0, len(files))
		for i, f := range files {
			weight := defaultWeights["style"]
			if cfg := lumaOpts.imageConfig(i); cfg.Weight != nil {
				weight = *cfg.Weight
			}
			images = append(images, map[string]interface{}{"url": f.URL, "weight": weight})
		}
		options["style"] = images

	case ReferenceTypeCharacter:
		identities := map[string][]string{}
		order := make([]string, 0, len(files))
		for i, f := range files {
			id := "identity0"
			if cfg := lumaOpts.imageConfig(i); cfg.ID != nil {
				id = *cfg.ID
			}
			if _, exists := identities[id]; !exists {
				order = append(order, id)
			}
			identities[id] = append(identities[id], f.URL)
		}
		for id, imgs := range identities {
			if len(imgs) > 4 {
				return nil, fmt.Errorf("Luma AI character supports up to 4 images per identity. Identity '%s' has %d images.", id, len(imgs))
			}
		}
		character := map[string]interface{}{}
		for _, id := range order {
			character[id] = map[string]interface{}{"images": identities[id]}
		}
		options["character"] = character

	case ReferenceTypeModifyImage:
		if len(files) > 1 {
			return nil, fmt.Errorf("Luma AI modify_image only supports a single input image. You provided %d images.", len(files))
		}
		weight := defaultWeights["modify_image"]
		if cfg := lumaOpts.imageConfig(0); cfg.Weight != nil {
			weight = *cfg.Weight
		}
		options["modify_image"] = map[string]interface{}{"url": files[0].URL, "weight": weight}
	}

	return options, nil
}

// parseErrorResponse converts a Luma error envelope
// ({detail: [{msg, ...}]}) into a provider error.
func (m *ImageModel) parseErrorResponse(body []byte, statusCode int) error {
	var errResp lumaErrorResponse
	msg := "Unknown error"
	if err := json.Unmarshal(body, &errResp); err == nil && len(errResp.Detail) > 0 && errResp.Detail[0].Msg != "" {
		msg = errResp.Detail[0].Msg
	}
	return providererrors.NewProviderError("luma", statusCode, "", msg, nil)
}

func flattenHeaders(headers map[string][]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// lumaGenerationResponse is a limited version of Luma's generation response
// schema, covering only what the implementation needs (TS
// lumaGenerationResponseSchema).
type lumaGenerationResponse struct {
	ID            string  `json:"id"`
	State         string  `json:"state"` // "queued" | "dreaming" | "completed" | "failed"
	FailureReason *string `json:"failure_reason"`
	Assets        *struct {
		Image string `json:"image"`
	} `json:"assets"`
}

type lumaErrorDetail struct {
	Type  string `json:"type"`
	Msg   string `json:"msg"`
	Input string `json:"input"`
}

type lumaErrorResponse struct {
	Detail []lumaErrorDetail `json:"detail"`
}
