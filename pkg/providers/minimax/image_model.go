package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	stdhttp "net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultImageBaseURL is MiniMax's image generation API root (TS
// imageBaseURL default).
const DefaultImageBaseURL = "https://api.minimax.io/v1"

// MiniMax image model IDs (TS MiniMaxImageModelId).
const (
	ModelH3Image          = "MiniMax-H3-Image"
	ModelH3ImageHighspeed = "MiniMax-H3-Image-Highspeed"
)

// MiniMaxImageModelOptions are the providerOptions.minimax settings of a
// MiniMax image model (TS MiniMaxImageModelOptions). Nil leaves a setting
// unset.
type MiniMaxImageModelOptions struct {
	// Area is the output area when a pixel size is not supplied: "1K",
	// "2K" or "4K". Defaults to 4K.
	Area *string `json:"area,omitempty"`
	// DisablePromptEnhanced disables prompt enhancement for text-to-image
	// requests.
	DisablePromptEnhanced *bool `json:"disablePromptEnhanced,omitempty"`
	// OutputCompression is the JPEG / WebP compression quality, 0 to 100.
	OutputCompression *int `json:"outputCompression,omitempty"`
	// OutputFormat is "jpeg", "jpg", "png" or "webp". Defaults to JPEG.
	OutputFormat *string `json:"outputFormat,omitempty"`
}

var (
	minimaxImageCompressionMin = 0
	minimaxImageCompressionMax = 100
)

// minimaxImageOptionsSchema is minimaxImageModelOptionsSchema.
var minimaxImageOptionsSchema = providerutils.OptionSchema{
	"area":                  providerutils.OptNullable(providerutils.OptEnum("1K", "2K", "4K")),
	"disablePromptEnhanced": providerutils.OptNullable(providerutils.OptBool),
	"outputCompression":     providerutils.OptNullable(providerutils.OptInt(&minimaxImageCompressionMin, &minimaxImageCompressionMax)),
	"outputFormat":          providerutils.OptNullable(providerutils.OptEnum("jpeg", "jpg", "png", "webp")),
}

// minimaxImageBodyOrder is the TS postJsonToApi body literal order.
var minimaxImageBodyOrder = map[string][]string{
	"": {"model", "prompt", "n", "size", "aspect_ratio", "area", "output_format", "output_compression", "disable_prompt_enhanced", "images", "response_format"},
}

// minimaxImageResponseNode is minimaxImageResponseSchema.
var minimaxImageResponseNode = func() zodlite.Node {
	nStr := zodlite.Nullish(zodlite.String)
	nNum := zodlite.Nullish(zodlite.Number)
	return zodlite.Object(
		zodlite.F("data", zodlite.Array(zodlite.Object(
			zodlite.F("url", nStr), zodlite.F("size", nStr), zodlite.F("revised_prompt", nStr),
			zodlite.F("error", zodlite.Nullish(zodlite.Object(zodlite.F("code", nStr), zodlite.F("message", zodlite.String)))),
		))),
		zodlite.F("output_format", nStr),
		zodlite.F("request_id", nStr),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("generated_images", nNum), zodlite.F("input_tokens", nNum), zodlite.F("output_tokens", nNum), zodlite.F("total_tokens", nNum),
			zodlite.F("input_tokens_details", zodlite.Nullish(zodlite.Object(zodlite.F("text_tokens", nNum), zodlite.F("image_tokens", nNum)))),
			zodlite.F("output_tokens_details", zodlite.Nullish(zodlite.Object(zodlite.F("image_tokens", nNum)))),
		))),
	)
}()

// minimaxImageUsageKeys are the declared usage keys (zod strips the rest).
var minimaxImageUsageKeys = map[string][]string{
	"":                      {"generated_images", "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details"},
	"input_tokens_details":  {"text_tokens", "image_tokens"},
	"output_tokens_details": {"image_tokens"},
}

// ImageModel implements provider.ImageModel for MiniMax H3 image generation
// and editing (TS MiniMaxImageModel, c4c8523cc3).
type ImageModel struct {
	prov    *Provider
	modelID string
}

func newImageModel(prov *Provider, modelID string) *ImageModel {
	return &ImageModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns "v4".
func (m *ImageModel) SpecificationVersion() string { return "v4" }

// Provider returns "minimax.image".
func (m *ImageModel) Provider() string { return "minimax.image" }

// ModelID returns the model ID.
func (m *ImageModel) ModelID() string { return m.modelID }

// MaxImagesPerCall returns 10 (the TS model catalog entry).
func (m *ImageModel) MaxImagesPerCall() int { return 10 }

// SupportsFileInputs returns true: files are edit references.
func (m *ImageModel) SupportsFileInputs() *bool { v := true; return &v }

// SupportsMaskInputs returns false: masks are rejected.
func (m *ImageModel) SupportsMaskInputs() *bool { v := false; return &v }

type minimaxImageResponse struct {
	Data []struct {
		URL           *string `json:"url"`
		Size          *string `json:"size"`
		RevisedPrompt *string `json:"revised_prompt"`
		Error         *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"data"`
	OutputFormat *string `json:"output_format"`
	RequestID    *string `json:"request_id"`
	Usage        *struct {
		InputTokens  *float64 `json:"input_tokens"`
		OutputTokens *float64 `json:"output_tokens"`
		TotalTokens  *float64 `json:"total_tokens"`
	} `json:"usage"`
}

// DoGenerate generates images (POST /images/generations) or, with files,
// edits them (POST /images/edits), then downloads every returned URL.
// Mirrors TS MiniMaxImageModel.doGenerate.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	var warnings []types.Warning
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	if opts.Mask != nil {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "MiniMax image masks"}
	}
	options, err := providerutils.ParseProviderOptions("minimax", providerutils.NormalizeOptionsJSON(opts.ProviderOptions), minimaxImageOptionsSchema)
	if err != nil {
		return nil, err
	}
	hasFiles := len(opts.Files) > 0
	if hasFiles && options["disablePromptEnhanced"] != nil {
		warnings = append(warnings, types.Warning{
			Type: "unsupported", Feature: "disablePromptEnhanced",
			Details: "MiniMax image edits always apply prompt enhancement.",
		})
	}
	timestamp := time.Now()

	// JSON.stringify drops undefined members; a nullish option is sent only
	// when it is not null (TS options?.x is undefined for an absent key and
	// null for an explicit null, which JSON.stringify keeps).
	body := map[string]interface{}{"model": m.modelID, "prompt": opts.Prompt}
	if opts.N != nil {
		body["n"] = *opts.N
	}
	if opts.Size != "" {
		body["size"] = opts.Size
	}
	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}
	for _, kv := range [][2]string{{"area", "area"}, {"outputFormat", "output_format"}, {"outputCompression", "output_compression"}} {
		if v, ok := options[kv[0]]; ok {
			body[kv[1]] = v
		}
	}
	path := "/images/generations"
	if hasFiles {
		path = "/images/edits"
		images := make([]interface{}, 0, len(opts.Files))
		for _, file := range opts.Files {
			images = append(images, map[string]interface{}{"image_url": minimaxImageFileToDataURI(file)})
		}
		body["images"] = images
	} else if v, ok := options["disablePromptEnhanced"]; ok {
		body["disable_prompt_enhanced"] = v
	}
	// URL output supports the full batch size without the base64 area limit.
	body["response_format"] = "url"

	headers, err := m.prov.resolveVideoHeaders(ctx, opts.Headers)
	if err != nil {
		return nil, err
	}
	resp, err := m.prov.imageClient.Do(ctx, internalhttp.Request{
		Method: stdhttp.MethodPost, Path: path, Headers: headers,
		Body: bytes.NewBufferString(providerutils.StringifyOrdered(body, minimaxImageBodyOrder)),
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err)
	}
	// TS response.text(): a BOM is dropped and invalid UTF-8 replaced
	// (WHATWG TextDecoder) before the body is parsed.
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, providererrors.NewSchemaProviderError("minimax.image",
			&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}, minimaxImageErrorSchema)
	}
	if err := providerutils.NodeResponseValidator("minimax.image", minimaxImageResponseNode)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var decoded minimaxImageResponse
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		return nil, fmt.Errorf("minimax: failed to decode image response: %w", err)
	}
	var raw struct {
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(resp.Body, &raw)

	type kept struct {
		url, size, revisedPrompt *string
	}
	var successful []kept
	for i, image := range decoded.Data {
		if image.Error != nil {
			warnings = append(warnings, types.Warning{Type: "other", Message: fmt.Sprintf("Image %d failed: %s", i+1, image.Error.Message)})
			continue
		}
		successful = append(successful, kept{image.URL, image.Size, image.RevisedPrompt})
	}

	images := make([][]byte, 0, len(successful))
	for _, image := range successful {
		if image.url == nil {
			return nil, fmt.Errorf("MiniMax returned an image without a URL.") //nolint:staticcheck // matches TS SDK exact error text
		}
		// TS getFromApi({validateUrl: true, trustedOrigin: baseURL}) without
		// headers: no credentials go to the image host.
		result, err := fileutil.DownloadWithMetadata(ctx, *image.url, fileutil.TrustedOriginDownloadOptions(m.prov.imageBaseURL, nil))
		if err != nil {
			return nil, providerutils.StatusCodeDownloadError("minimax.image", err)
		}
		images = append(images, result.Data)
	}

	var usageMeta interface{}
	if len(raw.Usage) > 0 && string(raw.Usage) != "null" {
		var u interface{}
		if json.Unmarshal(raw.Usage, &u) == nil {
			usageMeta = pickKeys(u, "", minimaxImageUsageKeys)
		}
	}
	metaImages := make([]interface{}, 0, len(successful))
	for i, image := range successful {
		entry := map[string]interface{}{"size": nil, "revisedPrompt": nil}
		if image.size != nil {
			entry["size"] = *image.size
		}
		if image.revisedPrompt != nil {
			entry["revisedPrompt"] = *image.revisedPrompt
		}
		if decoded.OutputFormat != nil {
			entry["outputFormat"] = *decoded.OutputFormat
		}
		if decoded.RequestID != nil {
			entry["requestId"] = *decoded.RequestID
		}
		// Core merges only per-image metadata; keep request usage once per call.
		if i == 0 && usageMeta != nil {
			entry["usage"] = usageMeta
		}
		metaImages = append(metaImages, entry)
	}

	result := &types.ImageResult{
		Images:   images,
		Warnings: warnings,
		Usage:    types.ImageUsage{ImageCount: len(images)},
		Response: &types.ResponseMetadata{Timestamp: timestamp, ModelID: m.modelID, Headers: providerutils.ExtractHeaders(resp.Headers)},
		ProviderMetadata: map[string]interface{}{
			"minimax": map[string]interface{}{"images": metaImages},
		},
	}
	if len(images) > 0 {
		result.Image = images[0]
	}
	if decoded.Usage != nil {
		result.Usage.InputTokens = tokenCount(decoded.Usage.InputTokens)
		result.Usage.OutputTokens = tokenCount(decoded.Usage.OutputTokens)
		result.Usage.TotalTokens = tokenCount(decoded.Usage.TotalTokens)
	}
	return result, nil
}

func tokenCount(v *float64) int {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return 0
	}
	return int(*v)
}

// pickKeys keeps the declared keys of a zod object output (unknown keys are
// stripped; null values are kept, as z.nullish does).
func pickKeys(v interface{}, path string, keys map[string][]string) interface{} {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	out := map[string]interface{}{}
	for _, key := range keys[path] {
		value, present := obj[key]
		if !present {
			continue
		}
		child := key
		if path != "" {
			child = path + "." + key
		}
		if _, nested := keys[child]; nested {
			value = pickKeys(value, child, keys)
		}
		out[key] = value
	}
	return out
}

// minimaxImageFileToDataURI is TS convertImageModelFileToDataUri: a URL is
// passed through and inline data becomes a data URI.
func minimaxImageFileToDataURI(file provider.ImageFile) string {
	if file.Type == "url" {
		return file.URL
	}
	return imageutil.ConvertToDataURI(file.Data, file.MediaType)
}

// minimaxImageErrorSchema is minimaxImageErrorSchema ({error: {message:
// string}}) with errorToMessage data.error.message.
func minimaxImageErrorSchema(body interface{}) (string, interface{}, bool) {
	obj, ok := body.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	errObj, ok := obj["error"].(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	message, ok := errObj["message"].(string)
	if !ok {
		return "", nil, false
	}
	return message, map[string]interface{}{"error": map[string]interface{}{"message": message}}, true
}
