package fal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	return "fal.image"
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

// MaxImagesPerCall mirrors TS FalImageModel#maxImagesPerCall.
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// falAspectRatioSizes mirrors TS convertAspectRatioToSize.
func falAspectRatioSize(aspectRatio string) interface{} {
	switch aspectRatio {
	case "1:1":
		return "square_hd"
	case "16:9":
		return "landscape_16_9"
	case "9:16":
		return "portrait_16_9"
	case "4:3":
		return "landscape_4_3"
	case "3:4":
		return "portrait_4_3"
	case "16:10":
		return map[string]interface{}{"width": 1280, "height": 800}
	case "10:16":
		return map[string]interface{}{"width": 800, "height": 1280}
	case "21:9":
		return map[string]interface{}{"width": 2560, "height": 1080}
	case "9:21":
		return map[string]interface{}{"width": 1080, "height": 2560}
	}
	return nil
}

func falFileToDataURI(f provider.ImageFile) string {
	if f.Type == "url" {
		return f.URL
	}
	return imageutil.ConvertToDataURI(f.Data, f.MediaType)
}

// falSnakeToCamel lists the deprecated snake_case provider options in the
// order TS falImageModelOptionsSchema maps them (the warning lists them in
// this order).
var falSnakeToCamel = [][2]string{
	{"image_url", "imageUrl"},
	{"mask_url", "maskUrl"},
	{"guidance_scale", "guidanceScale"},
	{"num_inference_steps", "numInferenceSteps"},
	{"enable_safety_checker", "enableSafetyChecker"},
	{"output_format", "outputFormat"},
	{"sync_mode", "syncMode"},
	{"safety_tolerance", "safetyTolerance"},
}

var falCamelToAPI = map[string]string{
	"imageUrl":            "image_url",
	"maskUrl":             "mask_url",
	"guidanceScale":       "guidance_scale",
	"numInferenceSteps":   "num_inference_steps",
	"enableSafetyChecker": "enable_safety_checker",
	"outputFormat":        "output_format",
	"syncMode":            "sync_mode",
	"safetyTolerance":     "safety_tolerance",
}

var falSnakeRe = regexp.MustCompile(`_([a-z])`)

// normalizeFalImageOptions mirrors the TS falImageModelOptionsSchema
// transform: snake_case keys win over camelCase and are reported as
// deprecated.
func normalizeFalImageOptions(raw map[string]interface{}) (map[string]interface{}, []string) {
	result := map[string]interface{}{}
	var deprecated []string
	known := map[string]bool{
		"strength": true, "acceleration": true, "useMultipleImages": true,
	}
	for _, pair := range falSnakeToCamel {
		snake, camel := pair[0], pair[1]
		known[snake], known[camel] = true, true
		if v := raw[snake]; v != nil {
			deprecated = append(deprecated, snake)
			result[camel] = v
		} else if v := raw[camel]; v != nil {
			result[camel] = v
		}
	}
	for _, k := range []string{"strength", "acceleration", "useMultipleImages"} {
		if v := raw[k]; v != nil {
			result[k] = v
		}
	}
	for k, v := range raw {
		if !known[k] {
			result[k] = v
		}
	}
	return result, deprecated
}

// DoGenerate performs image generation (TS FalImageModel#doGenerate).
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	if err := validateImageOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	reqBody, warnings, err := m.buildRequest(opts)
	if err != nil {
		return nil, err
	}
	currentDate := time.Now()

	path := fmt.Sprintf("/%s", m.modelID)
	resp, err := m.provider.client.Do(ctx, internalhttp.Request{Method: "POST", Path: path, Body: reqBody, Headers: opts.Headers})
	if err != nil {
		return nil, providererrors.NewProviderError("fal", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, m.statusError(resp.StatusCode, resp.Headers, resp.Body)
	}
	if err := providerutils.MediaResponseValidator("fal", providerutils.SchemaFalImage)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	result, err := m.convertBody(ctx, resp.Body)
	if err != nil {
		return nil, err
	}
	result.Warnings = warnings
	result.Response = &types.ResponseMetadata{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   providerutils.ExtractHeaders(resp.Headers),
	}
	return result, nil
}

// statusError builds a ProviderError for a non-2xx response using TS
// falFailedResponseHandler's message rules (validation detail list, else
// message).
func (m *ImageModel) statusError(status int, headers http.Header, body []byte) error {
	err := internalhttp.StatusProviderError("fal", status, headers, body)
	var parsed struct {
		Detail  json.RawMessage `json:"detail"`
		Message *string         `json:"message"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return err
	}
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		return err
	}
	var details []struct {
		Loc []interface{} `json:"loc"`
		Msg string        `json:"msg"`
	}
	if len(parsed.Detail) > 0 && json.Unmarshal(parsed.Detail, &details) == nil && details != nil {
		lines := make([]string, 0, len(details))
		for _, d := range details {
			loc := make([]string, 0, len(d.Loc))
			for _, l := range d.Loc {
				loc = append(loc, fmt.Sprint(l))
			}
			lines = append(lines, strings.Join(loc, ".")+": "+d.Msg)
		}
		pe.Message = strings.Join(lines, "\n")
	} else if parsed.Message != nil {
		pe.Message = *parsed.Message
	}
	return pe
}

// buildRequest builds the request body and warnings (TS getArgs).
func (m *ImageModel) buildRequest(opts *provider.ImageGenerateOptions) (map[string]interface{}, []types.Warning, error) {
	warnings := []types.Warning{}
	body := map[string]interface{}{"prompt": opts.Prompt}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	if opts.N != nil {
		body["num_images"] = *opts.N
	}
	if opts.Size != "" {
		var width, height int
		if _, err := fmt.Sscanf(opts.Size, "%dx%d", &width, &height); err == nil {
			body["image_size"] = map[string]interface{}{"width": width, "height": height}
		}
	} else if opts.AspectRatio != "" {
		if size := falAspectRatioSize(opts.AspectRatio); size != nil {
			body["image_size"] = size
		}
	}

	var falOpts map[string]interface{}
	var deprecated []string
	if raw, ok := opts.ProviderOptions["fal"]; ok && raw != nil {
		rawMap, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("fal: invalid providerOptions.fal: expected object")
		}
		falOpts, deprecated = normalizeFalImageOptions(rawMap)
	}

	if len(opts.Files) > 0 {
		if falOpts["useMultipleImages"] == true {
			urls := make([]string, len(opts.Files))
			for i, f := range opts.Files {
				urls[i] = falFileToDataURI(f)
			}
			body["image_urls"] = urls
		} else {
			body["image_url"] = falFileToDataURI(opts.Files[0])
			if len(opts.Files) > 1 {
				warnings = append(warnings, types.Warning{
					Type: "other",
					Message: "Multiple input images provided but useMultipleImages is not enabled. " +
						"Only the first image will be used. Set providerOptions.fal.useMultipleImages " +
						"to true for models that support multiple images (e.g., fal-ai/flux-2/edit).",
				})
			}
		}
	}

	if opts.Mask != nil {
		body["mask_url"] = falFileToDataURI(*opts.Mask)
	}

	if len(deprecated) > 0 {
		parts := make([]string, len(deprecated))
		for i, key := range deprecated {
			camel := falSnakeRe.ReplaceAllStringFunc(key, func(s string) string { return strings.ToUpper(s[1:]) })
			parts[i] = fmt.Sprintf("'%s' (use '%s')", key, camel)
		}
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "The following provider options use deprecated snake_case and will be removed in @ai-sdk/fal v2.0. Please use camelCase instead: " + strings.Join(parts, ", "),
		})
	}

	for key, value := range falOpts {
		if key == "useMultipleImages" {
			continue
		}
		apiKey := key
		if mapped, ok := falCamelToAPI[key]; ok {
			apiKey = mapped
		}
		body[apiKey] = value
	}

	return body, warnings, nil
}

// convertBody parses the response, downloads every image and builds the
// result, including providerMetadata.fal (TS doGenerate).
func (m *ImageModel) convertBody(ctx context.Context, raw []byte) (*types.ImageResult, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var rawImages []map[string]interface{}
	if imgs, ok := top["images"]; ok {
		if err := json.Unmarshal(imgs, &rawImages); err != nil {
			return nil, fmt.Errorf("failed to decode response images: %w", err)
		}
	} else if one, ok := top["image"]; ok {
		var img map[string]interface{}
		if err := json.Unmarshal(one, &img); err != nil {
			return nil, fmt.Errorf("failed to decode response image: %w", err)
		}
		rawImages = []map[string]interface{}{img}
	}
	if len(rawImages) == 0 {
		if _, hasImages := top["images"]; !hasImages {
			return nil, fmt.Errorf("no images generated")
		}
		// TS accepts an empty `images` array and returns it empty; core
		// retries and then throws NoImageGeneratedError.
		return &types.ImageResult{}, nil
	}

	var hasNSFW, nsfwDetected []bool
	if v, ok := top["has_nsfw_concepts"]; ok {
		_ = json.Unmarshal(v, &hasNSFW)
	}
	if v, ok := top["nsfw_content_detected"]; ok {
		_ = json.Unmarshal(v, &nsfwDetected)
	}

	images := make([][]byte, 0, len(rawImages))
	imagesMeta := make([]map[string]interface{}, 0, len(rawImages))
	firstType := ""
	firstURL := ""
	for i, img := range rawImages {
		url, _ := img["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("fal image %d is missing a url", i)
		}
		data, err := m.downloadImage(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to download image: %w", err)
		}
		images = append(images, data)

		meta := map[string]interface{}{}
		for k, v := range img {
			switch k {
			case "url", "content_type", "file_name", "file_data", "file_size":
			case "width", "height":
				meta[k] = v
			}
		}
		if v, ok := img["content_type"]; ok && v != nil {
			meta["contentType"] = v
		}
		if v, ok := img["file_name"]; ok && v != nil {
			meta["fileName"] = v
		}
		if v, ok := img["file_data"]; ok && v != nil {
			meta["fileData"] = v
		}
		if v, ok := img["file_size"]; ok && v != nil {
			meta["fileSize"] = v
		}
		if i < len(hasNSFW) {
			meta["nsfw"] = hasNSFW[i]
		} else if i < len(nsfwDetected) {
			meta["nsfw"] = nsfwDetected[i]
		}
		imagesMeta = append(imagesMeta, meta)
		if i == 0 {
			firstURL = url
			firstType, _ = img["content_type"].(string)
		}
	}

	falMeta := map[string]interface{}{"images": imagesMeta}
	for k, v := range top {
		switch k {
		case "images", "prompt", "has_nsfw_concepts", "nsfw_content_detected":
			continue
		}
		var decoded interface{}
		if json.Unmarshal(v, &decoded) == nil {
			falMeta[k] = decoded
		}
	}

	if firstType == "" {
		firstType = "image/png"
	}
	return &types.ImageResult{
		Image:            images[0],
		Images:           images,
		MimeType:         firstType,
		URL:              firstURL,
		Usage:            types.ImageUsage{ImageCount: len(images)},
		ProviderMetadata: map[string]interface{}{"fal": falMeta},
	}, nil
}

// downloadImage fetches a generated image. The URL comes from the response
// body, so it is SSRF-validated (the configured base URL origin is exempt)
// and no credentials are sent (TS getFromApi validateUrl + trustedOrigin).
func (m *ImageModel) downloadImage(ctx context.Context, url string) ([]byte, error) {
	baseURL := m.provider.config.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	opts := fileutil.TrustedOriginDownloadOptions(baseURL, m.provider.client.HTTPClient().Transport)
	opts.Timeout = 30 * time.Second
	return fileutil.Download(ctx, url, opts)
}
