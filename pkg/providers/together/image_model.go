package together

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Together AI
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Together AI image generation model
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
	return "together"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts.Mask != nil {
		return nil, fmt.Errorf("together AI does not support mask-based image editing")
	}

	reqBody := m.buildRequestBody(opts)

	resp, err := m.provider.client.Post(ctx, "/v1/images/generations", reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("together", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("together AI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	result, err := m.convertResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, togetherWarnings(m.modelID, opts)...)
	return result, nil
}

// nonDiffusionImageModels lists Together image models that aren't diffusion
// models under the hood and so reject diffusion-only parameters
// (steps/guidance/negative_prompt/disable_safety_checker/seed).
var nonDiffusionImageModels = map[string]bool{
	"google/gemini-3-pro-image": true,
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"model":           m.modelID,
		"prompt":          opts.Prompt,
		"response_format": "base64",
	}

	isNonDiffusion := nonDiffusionImageModels[m.modelID]

	if opts.N != nil && *opts.N > 1 {
		reqBody["n"] = *opts.N
	}

	if opts.Seed != nil && !isNonDiffusion {
		reqBody["seed"] = *opts.Seed
	}

	if opts.Size != "" {
		var width, height int
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height) //nolint:errcheck
		if width > 0 && height > 0 {
			reqBody["width"] = width
			reqBody["height"] = height
		}
	}

	if len(opts.Files) > 0 {
		reqBody["image_url"] = togetherImageFileToDataURI(opts.Files[0])
	}

	if togetherOpts, ok := opts.ProviderOptions["togetherai"].(map[string]interface{}); ok {
		for key, value := range togetherOpts {
			if isNonDiffusion && nonDiffusionExcludedOptions[key] {
				continue
			}
			reqBody[key] = value
		}
	}

	return reqBody
}

func togetherImageFileToDataURI(file provider.ImageFile) string {
	if file.Type == "url" || file.URL != "" {
		return file.URL
	}
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data))
}

// nonDiffusionExcludedOptions lists providerOptions.togetherai keys stripped
// for nonDiffusionImageModels (they're diffusion-only parameters that a
// non-diffusion model like google/gemini-3-pro-image rejects).
var nonDiffusionExcludedOptions = map[string]bool{
	"steps":                  true,
	"guidance":               true,
	"negative_prompt":        true,
	"disable_safety_checker": true,
}

func togetherWarnings(modelID string, opts *provider.ImageGenerateOptions) []types.Warning {
	if opts == nil {
		return nil
	}
	var warnings []types.Warning
	if opts.Size != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "This model does not support the `aspectRatio` option. Use `size` instead.",
		})
	}
	if nonDiffusionImageModels[modelID] && opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
			Details: fmt.Sprintf("The %s model does not support the `seed` option.", modelID),
		})
	}
	if len(opts.Files) > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "Together AI only supports a single input image. Additional images are ignored.",
		})
	}
	return warnings
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

	for _, item := range response.Data {
		if item.URL != "" {
			if result.URL == "" {
				result.URL = item.URL
			}
			result.MimeType = "image/png"
		}
		if item.B64JSON != "" {
			image := []byte(item.B64JSON)
			result.Images = append(result.Images, image)
			result.Base64Images = append(result.Base64Images, item.B64JSON)
			if result.Image == nil {
				result.Image = image
				result.Base64Image = item.B64JSON
			}
			result.MimeType = "image/png"
		}
	}

	if result.Usage.ImageCount == 0 {
		result.Usage.ImageCount = len(response.Data)
	}

	return result, nil
}
