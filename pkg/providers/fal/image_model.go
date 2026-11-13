package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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
		return falImageSize(1280, 800)
	case "10:16":
		return falImageSize(800, 1280)
	case "21:9":
		return falImageSize(2560, 1080)
	case "9:21":
		return falImageSize(1080, 2560)
	}
	return nil
}

// falImageSize is TS {width, height} in that key order.
func falImageSize(width, height interface{}) *jsonorder.Object {
	o := jsonorder.New()
	o.Set("width", width)
	o.Set("height", height)
	return o
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

// normalizeFalImageOptionsOrdered mirrors the TS falImageModelOptionsSchema
// transform (snake_case keys win over camelCase and are reported as
// deprecated) and returns the key order of the transform result: the mapped
// keys in mapKey order, then
// strength, acceleration and useMultipleImages, then the passthrough keys
// (sorted: a Go map has no input order).
func normalizeFalImageOptionsOrdered(raw map[string]interface{}) (map[string]interface{}, []string, []string) {
	result := map[string]interface{}{}
	var order []string
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
			order = append(order, camel)
		} else if v := raw[camel]; v != nil {
			result[camel] = v
			order = append(order, camel)
		}
	}
	for _, k := range []string{"strength", "acceleration", "useMultipleImages"} {
		if v := raw[k]; v != nil {
			result[k] = v
			order = append(order, k)
		}
	}
	var passthrough []string
	for k, v := range raw {
		if !known[k] {
			result[k] = v
			passthrough = append(passthrough, k)
		}
	}
	sort.Strings(passthrough)
	order = append(order, passthrough...)
	return result, order, deprecated
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
		return nil, err // TS postJsonToApi lets a transport failure propagate unchanged
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, m.statusError(resp.StatusCode, resp.Headers, resp.Body)
	}
	if err := providerutils.MediaResponseValidator("fal", providerutils.SchemaFalImage)(resp.StatusCode, resp.Headers, []byte(textdecode.UTF8(resp.Body))); err != nil {
		return nil, err
	}

	result, err := m.convertBody(ctx, []byte(textdecode.UTF8(resp.Body)))
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
	cause := &internalhttp.HTTPStatusError{StatusCode: status, Headers: headers, Body: body}
	perr := providererrors.NewProviderErrorFromResponse("fal", 0, "", falImageErrorMessage(status, body), cause)
	perr.Data = falImageErrorData(body)
	return perr
}

// falImageErrorData is the zod output of the image model's falErrorSchema
// union for a matching body: {detail: [{loc, msg, type}]} or {message},
// undeclared keys stripped; nil for an off-schema body (TS data undefined).
func falImageErrorData(body []byte) interface{} {
	var top map[string]interface{}
	if securejson.Unmarshal([]byte(textdecode.UTF8(body)), &top) != nil || top == nil {
		return nil
	}
	if detail, ok := top["detail"].([]interface{}); ok {
		entries := make([]interface{}, 0, len(detail))
		valid := true
		for _, raw := range detail {
			d, _ := raw.(map[string]interface{})
			loc, okLoc := d["loc"].([]interface{})
			msg, okMsg := d["msg"].(string)
			typ, okType := d["type"].(string)
			if !okLoc || !okMsg || !okType {
				valid = false
				break
			}
			for _, l := range loc {
				if _, isStr := l.(string); !isStr {
					valid = false
				}
			}
			if !valid {
				break
			}
			entries = append(entries, map[string]interface{}{"loc": loc, "msg": msg, "type": typ})
		}
		if valid {
			return map[string]interface{}{"detail": entries}
		}
	}
	if message, ok := top["message"].(string); ok {
		return map[string]interface{}{"message": message}
	}
	return nil
}

// falImageErrorMessage mirrors the image model's TS falFailedResponseHandler:
// the body must match falErrorSchema, a union of the validation shape
// {detail:[{loc:string[],msg:string,type:string}]} (message: the entries as
// "loc.joined: msg" lines) and {message:string}; any other body gives the
// HTTP status text.
func falImageErrorMessage(status int, body []byte) string {
	// Decode field by field: a mistyped `detail` (a string) must not hide a
	// valid `message`, which the second union branch still accepts.
	var top map[string]json.RawMessage
	if securejson.Unmarshal([]byte(textdecode.UTF8(body)), &top) != nil || top == nil {
		return http.StatusText(status)
	}
	var detail []json.RawMessage
	if raw, ok := top["detail"]; ok && json.Unmarshal(raw, &detail) == nil && detail != nil {
		lines := make([]string, 0, len(detail))
		valid := true
		for _, raw := range detail {
			var d struct {
				Loc  *[]string `json:"loc"`
				Msg  *string   `json:"msg"`
				Type *string   `json:"type"`
			}
			if jsonexact.Unmarshal(raw, &d) != nil || d.Loc == nil || d.Msg == nil || d.Type == nil {
				valid = false
				break
			}
			lines = append(lines, strings.Join(*d.Loc, ".")+": "+*d.Msg)
		}
		if valid {
			return strings.Join(lines, "\n")
		}
	}
	var message *string
	if raw, ok := top["message"]; ok && json.Unmarshal(raw, &message) == nil && message != nil {
		return *message
	}
	return http.StatusText(status)
}

// buildRequest builds the request body and warnings (TS getArgs), in the
// TS requestBody insertion order: prompt, seed, image_size, num_images, the
// file inputs, then the options in transform order.
func (m *ImageModel) buildRequest(opts *provider.ImageGenerateOptions) (*jsonorder.Object, []types.Warning, error) {
	warnings := []types.Warning{}
	body := jsonorder.New()
	body.Set("prompt", opts.Prompt)
	if opts.Seed != nil {
		body.Set("seed", *opts.Seed)
	}
	var imageSize interface{}
	if opts.Size != "" {
		// TS size.split('x').map(Number): each side is JS Number() of its
		// text (an unreadable one is NaN, written as null; a fraction stays
		// a fraction). A missing height is undefined, which JSON.stringify
		// omits.
		parts := strings.Split(opts.Size, "x")
		side := func(i int) interface{} {
			if i >= len(parts) {
				return nil
			}
			f, ok := providerutils.JSNumber(parts[i])
			if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil
			}
			if f == math.Trunc(f) && math.Abs(f) < 1e15 {
				if f >= math.MinInt && f <= math.MaxInt {
					return int(f) // the v0.5.2 type
				}
				return int64(f)
			}
			return f
		}
		size := falImageSize(side(0), side(1))
		if len(parts) < 2 {
			size.Delete("height")
		}
		imageSize = size
	} else if opts.AspectRatio != "" {
		if size := falAspectRatioSize(opts.AspectRatio); size != nil {
			imageSize = size
		}
	}
	if imageSize != nil {
		body.Set("image_size", imageSize)
	}
	if opts.N != nil {
		body.Set("num_images", *opts.N)
	}

	var falOpts map[string]interface{}
	var falOrder []string
	var deprecated []string
	if raw, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["fal"]; ok && raw != nil {
		rawMap, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("fal: invalid providerOptions.fal: expected object")
		}
		falOpts, falOrder, deprecated = normalizeFalImageOptionsOrdered(rawMap)
	}

	if len(opts.Files) > 0 {
		if falOpts["useMultipleImages"] == true {
			urls := make([]string, len(opts.Files))
			for i, f := range opts.Files {
				urls[i] = falFileToDataURI(f)
			}
			body.Set("image_urls", urls)
		} else {
			body.Set("image_url", falFileToDataURI(opts.Files[0]))
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
		body.Set("mask_url", falFileToDataURI(*opts.Mask))
	}

	if len(deprecated) > 0 {
		parts := make([]string, len(deprecated))
		for i, key := range deprecated {
			camel := falSnakeRe.ReplaceAllStringFunc(key, func(s string) string { return providerutils.JSToUpper(s[1:]) })
			parts[i] = fmt.Sprintf("'%s' (use '%s')", key, camel)
		}
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "The following provider options use deprecated snake_case and will be removed in @ai-sdk/fal v2.0. Please use camelCase instead: " + strings.Join(parts, ", "),
		})
	}

	for _, key := range falOrder {
		if key == "useMultipleImages" {
			continue
		}
		apiKey := key
		if mapped, ok := falCamelToAPI[key]; ok {
			apiKey = mapped
		}
		body.Set(apiKey, falOpts[key])
	}

	return falReservedOrder(body), warnings, nil
}

// falReservedOrder is the TS requestBody literal: prompt, seed, image_size
// and num_images hold the first positions even when undefined, so an option
// that sets one of them later (e.g. providerOptions.fal.image_size) is
// written there, not after the other options.
func falReservedOrder(body *jsonorder.Object) *jsonorder.Object {
	out := jsonorder.New()
	for _, k := range []string{"prompt", "seed", "image_size", "num_images"} {
		if v, ok := body.Get(k); ok {
			out.Set(k, v)
		}
	}
	for _, k := range body.Keys() {
		if _, done := out.Get(k); !done {
			v, _ := body.Get(k)
			out.Set(k, v)
		}
	}
	return out
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
		if err := jsonexact.Unmarshal(imgs, &rawImages); err != nil {
			return nil, fmt.Errorf("failed to decode response images: %w", err)
		}
	} else if one, ok := top["image"]; ok {
		var img map[string]interface{}
		if err := jsonexact.Unmarshal(one, &img); err != nil {
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
			// TS getFromApi with createStatusCodeErrorResponseHandler.
			return nil, providerutils.StatusCodeDownloadError("fal", err)
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
		// TS removeOnlyUndefined: a present key stays, an explicit null
		// included (content_type, file_name and file_size are nullish).
		for wire, key := range map[string]string{"content_type": "contentType", "file_name": "fileName", "file_data": "fileData", "file_size": "fileSize"} {
			if v, ok := img[wire]; ok {
				meta[key] = v
			}
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
		if jsonexact.Unmarshal(v, &decoded) == nil {
			falMeta[k] = stripFalNested(k, decoded)
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
	opts.Timeout = fileutil.NoTimeout // TS getFromApi sets no timeout
	data, err := fileutil.Download(ctx, url, opts)
	// TS getFromApi handleFetchError: a network failure is the retryable
	// "Cannot connect to API" error, an abort stays as is.
	if cause, ok := fileutil.TransportFailure(err); ok {
		return nil, internalhttp.TransportErrorCtx(ctx, cause)
	}
	return data, err
}

// falNestedKeys are the keys the TS falImageResponseSchema declares for its
// nested z.object schemas. Zod strips every other key of those objects (only
// the top-level looseObject passes unknown keys through), so the metadata
// carries just these.
var falNestedKeys = map[string][]string{
	"timings":                {"inference"},
	"debug_latents":          {"url", "content_type", "file_name", "file_data", "file_size"},
	"debug_per_pass_latents": {"url", "content_type", "file_name", "file_data", "file_size"},
	"image":                  {"url", "width", "height", "content_type", "file_name", "file_data", "file_size"},
}

func stripFalNested(key string, v interface{}) interface{} {
	allowed, ok := falNestedKeys[key]
	obj, isMap := v.(map[string]interface{})
	if !ok || !isMap {
		return v
	}
	out := make(map[string]interface{}, len(allowed))
	for _, k := range allowed {
		if val, present := obj[k]; present {
			out[k] = val
		}
	}
	return out
}
