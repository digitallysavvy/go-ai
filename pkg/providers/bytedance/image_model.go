package bytedance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for ByteDance's
// Seedream image models (TS ByteDanceImageModel).
type ImageModel struct {
	prov    *Provider
	modelID string
}

// newImageModel creates a new ByteDance Seedream image generation model.
func newImageModel(prov *Provider, modelID string) *ImageModel {
	return &ImageModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *ImageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name.
func (m *ImageModel) Provider() string {
	return "bytedance.image"
}

// ModelID returns the model ID.
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// MaxImagesPerCall returns 1: the API has no output-count parameter, so a
// single call returns one image; generateImage fans N out into N calls.
// Batches of related images are available via the sequentialImageGeneration
// provider option instead (TS ByteDanceImageModel#maxImagesPerCall).
func (m *ImageModel) MaxImagesPerCall() *int {
	one := 1
	return &one
}

// byteDanceFileInputModels lists the known ByteDance model IDs that accept
// file inputs for image editing (TS ByteDanceImageModel#supportsFileInputs).
var byteDanceFileInputModels = map[string]bool{
	"dola-seedream-5-0-pro-260628": true,
	"seedream-5-0-260128":          true,
	"seedream-5-0-lite-260128":     true,
	"seedream-4-5-251128":          true,
	"seedream-4-0-250828":          true,
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if byteDanceFileInputModels[m.modelID] {
		return boolPtr(true)
	}
	return nil
}

// SupportsMaskInputs reports whether the model accepts a mask input for
// inpainting. ByteDance's file-input models do not support masks.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if v := m.SupportsFileInputs(); v != nil && *v {
		return boolPtr(false)
	}
	return nil
}

func boolPtr(b bool) *bool {
	return &b
}

// DoGenerate generates an image via ByteDance's /images/generations endpoint
// (TS ByteDanceImageModel#doGenerate).
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	warnings := []types.Warning{}

	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "ByteDance does not support aspectRatio. Use `size` (e.g. \"2048x2048\" " +
				"or a resolution level like \"2K\" via providerOptions) instead.",
		})
	}

	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}

	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "mask",
			Details: "ByteDance Seedream does not support a separate mask. Provide edit " +
				"instructions in the prompt, optionally with markings on the input image.",
		})
	}

	byteDanceOptions, err := extractImageProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	body, err := m.buildRequestBody(opts, byteDanceOptions)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/images/generations",
		Headers: opts.Headers,
		Body:    body,
	})
	if err != nil {
		return nil, fmt.Errorf("bytedance: image generation request failed: %w", err)
	}
	if httpResp.StatusCode >= 400 {
		return nil, m.parseAPIError(httpResp.Body, httpResp.StatusCode)
	}

	var imgResp byteDanceImageResponse
	if err := json.Unmarshal(httpResp.Body, &imgResp); err != nil {
		return nil, fmt.Errorf("bytedance: failed to parse image response: %w", err)
	}

	images := make([]string, 0, len(imgResp.Data))
	for _, item := range imgResp.Data {
		images = append(images, item.B64JSON)
	}

	result := &types.ImageResult{
		Warnings:     warnings,
		Base64Images: images,
		Usage:        mapImageUsage(imgResp.Usage),
		Response: &types.ResponseMetadata{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   flattenHeaders(httpResp.Headers),
		},
	}
	if len(images) > 0 {
		result.Base64Image = images[0]
	}
	return result, nil
}

// buildRequestBody builds the /images/generations request body from call
// options and typed provider options (TS doGenerate body construction).
func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions, byteDanceOptions *ImageModelOptions) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	if len(opts.Files) > 0 {
		if len(opts.Files) == 1 {
			encoded, err := encodeImageFile(&opts.Files[0])
			if err != nil {
				return nil, err
			}
			body["image"] = encoded
		} else {
			encodedImages := make([]string, 0, len(opts.Files))
			for i := range opts.Files {
				encoded, err := encodeImageFile(&opts.Files[i])
				if err != nil {
					return nil, err
				}
				encodedImages = append(encodedImages, encoded)
			}
			body["image"] = encodedImages
		}
	}

	if opts.Size != "" {
		body["size"] = opts.Size
	}

	if byteDanceOptions.Watermark != nil {
		body["watermark"] = *byteDanceOptions.Watermark
	}
	if byteDanceOptions.OutputFormat != nil {
		body["output_format"] = *byteDanceOptions.OutputFormat
	}
	// A resolution level (e.g. "2K") overrides the top-level pixel size.
	if byteDanceOptions.Size != nil {
		body["size"] = *byteDanceOptions.Size
	}
	if byteDanceOptions.SequentialImageGeneration != nil {
		body["sequential_image_generation"] = *byteDanceOptions.SequentialImageGeneration
	}
	if byteDanceOptions.MaxImages != nil {
		body["sequential_image_generation_options"] = map[string]interface{}{
			"max_images": *byteDanceOptions.MaxImages,
		}
	}
	if byteDanceOptions.OptimizePromptMode != nil {
		body["optimize_prompt_options"] = map[string]interface{}{
			"mode": *byteDanceOptions.OptimizePromptMode,
		}
	}
	// Pass through any additional options not explicitly handled above.
	for k, v := range byteDanceOptions.Additional {
		body[k] = v
	}

	// Always request base64 so the SDK receives the image bytes; this is not
	// user-overridable.
	body["response_format"] = "b64_json"

	return body, nil
}

// parseAPIError parses a ByteDance image API error response (TS
// byteDanceFailedResponseHandler).
func (m *ImageModel) parseAPIError(body []byte, statusCode int) error {
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		msg := ""
		if errResp.Error != nil {
			msg = errResp.Error.Message
		} else if errResp.Message != "" {
			msg = errResp.Message
		}
		if msg != "" {
			return NewError(statusCode, msg, "")
		}
	}
	return NewError(statusCode, fmt.Sprintf("API returned status %d", statusCode), string(body))
}

// encodeImageFile encodes a provider.ImageFile to the string form ByteDance
// expects: a URL is passed through unchanged, and inline data is encoded as
// a data URI (TS convertImageModelFileToDataUri).
func encodeImageFile(f *provider.ImageFile) (string, error) {
	if f.Type == "url" {
		return f.URL, nil
	}
	if f.Type == "file" {
		return imageutil.ConvertToDataURI(f.Data, f.MediaType), nil
	}
	return "", fmt.Errorf("bytedance: unsupported image file type: %s", f.Type)
}

// mapImageUsage maps ByteDance's Ark token usage (when present) to
// types.ImageUsage. Ark reports no input token count for image generation.
func mapImageUsage(usage *byteDanceImageUsage) types.ImageUsage {
	if usage == nil {
		return types.ImageUsage{}
	}
	result := types.ImageUsage{}
	if usage.OutputTokens != nil {
		result.OutputTokens = *usage.OutputTokens
	}
	if usage.TotalTokens != nil {
		result.TotalTokens = *usage.TotalTokens
	}
	return result
}

// byteDanceImageResponse is the response body from /images/generations.
// Minimal schema focused on what the implementation needs (TS
// byteDanceImageResponseSchema).
type byteDanceImageResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Usage *byteDanceImageUsage `json:"usage"`
}

type byteDanceImageUsage struct {
	OutputTokens *int `json:"output_tokens"`
	TotalTokens  *int `json:"total_tokens"`
}

// errorResponse and errorDetail are shared with video_model.go's ByteDance
// error envelope.
