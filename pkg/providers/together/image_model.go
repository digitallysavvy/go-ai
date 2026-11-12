package together

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"

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
	return "togetherai.image"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts.Mask != nil {
		// TS throws this exact text.
		return nil, errors.New("Together AI does not support mask-based image editing. " + //nolint:staticcheck // matches TS SDK's exact error text
			"Use FLUX Kontext models (e.g., black-forest-labs/FLUX.1-kontext-pro) " +
			"with a reference image and descriptive prompt instead.")
	}

	if err := validateImageOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}

	reqBody := m.buildRequestBody(opts)

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{Method: "POST", Path: "/v1/images/generations", Body: reqBody, Headers: opts.Headers})
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
		}
		// TS handleFetchError: a failure without an HTTP response (abort, deadline, the "Cannot connect to API" error) is returned unchanged
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, providererrors.NewSchemaProviderError("together", &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}, togetherErrorSchema)
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text(): BOM dropped, invalid UTF-8 replaced
	if err := providerutils.MediaResponseValidator("together", providerutils.SchemaTogetherImage)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	result, err := m.convertResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, togetherWarnings(m.modelID, opts)...)
	result.Response = &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: providerutils.ExtractHeaders(resp.Headers)}
	return result, nil
}

// MaxImagesPerCall mirrors TS TogetherAIImageModel#maxImagesPerCall.
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// SupportsFileInputs mirrors TS TogetherAIImageModel#supportsFileInputs.
func (m *ImageModel) SupportsFileInputs() *bool {
	switch m.modelID {
	case "black-forest-labs/FLUX.1-kontext-pro", "black-forest-labs/FLUX.1-kontext-max",
		"black-forest-labs/FLUX.1-kontext-dev", "black-forest-labs/FLUX.1-canny",
		"black-forest-labs/FLUX.1-depth", "black-forest-labs/FLUX.1-redux",
		"black-forest-labs/FLUX.2-pro", "black-forest-labs/FLUX.2-flex":
		t := true
		return &t
	case "stabilityai/stable-diffusion-xl-base-1.0", "black-forest-labs/FLUX.1-dev",
		"black-forest-labs/FLUX.1-dev-lora", "black-forest-labs/FLUX.1-schnell",
		"black-forest-labs/FLUX.1.1-pro", "black-forest-labs/FLUX.1-pro",
		"black-forest-labs/FLUX.1-schnell-Free", "black-forest-labs/FLUX.2-dev",
		"google/gemini-3-pro-image":
		f := false
		return &f
	}
	return nil
}

// SupportsMaskInputs mirrors TS TogetherAIImageModel#supportsMaskInputs.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if m.SupportsFileInputs() == nil {
		return nil
	}
	f := false
	return &f
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
		// TS size.split('x') and parseInt of each part; NaN (or a missing
		// part) is sent as null (togetherai-image-model.ts:144-157).
		parts := strings.Split(opts.Size, "x")
		height := ""
		if len(parts) > 1 {
			height = parts[1]
		}
		reqBody["width"] = togetherParseInt(parts[0])
		reqBody["height"] = togetherParseInt(height)
	}

	if len(opts.Files) > 0 {
		reqBody["image_url"] = togetherImageFileToDataURI(opts.Files[0])
	}

	// Read through the JSON form (TS options are JSON objects), so a typed
	// literal such as map[string]int is sent like the map[string]interface{}
	// form.
	if togetherOpts, ok := providerutils.NormalizeProviderOptionsObject(opts.ProviderOptions["togetherai"]); ok {
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
		// TS returns an empty `images` array; core retries and then throws
		// NoImageGeneratedError, so an empty response is not a provider error.
		return &types.ImageResult{}, nil
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
			image, err := base64.StdEncoding.DecodeString(item.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 image: %w", err)
			}
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

// togetherParseInt is JS parseInt(s, 10) as JSON: leading whitespace, an
// optional sign and the leading digits; nil (JSON null for NaN) when there
// are none.
func togetherParseInt(s string) interface{} {
	s = strings.TrimLeft(s, " \t\n\r\v\f\u00a0\ufeff")
	sign := 1
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	n, digits := 0, 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		digits++
	}
	if digits == 0 {
		return nil
	}
	return sign * n
}
