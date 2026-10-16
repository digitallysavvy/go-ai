package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for OpenAI DALL-E
type ImageModel struct {
	provider *Provider
	modelID  string
}

// OpenAIImageModelOptions contains OpenAI image-generation specific options.
type OpenAIImageModelOptions struct {
	Quality           string `json:"quality,omitempty"`
	Size              string `json:"size,omitempty"`
	Style             string `json:"style,omitempty"`
	Background        string `json:"background,omitempty"`
	Moderation        string `json:"moderation,omitempty"`
	OutputFormat      string `json:"outputFormat,omitempty"`
	OutputCompression *int   `json:"outputCompression,omitempty"`
	InputFidelity     string `json:"inputFidelity,omitempty"`
	User              string `json:"user,omitempty"`
}

// OpenAIImageProviderOptions is kept as an alias for backward compatibility.
type OpenAIImageProviderOptions = OpenAIImageModelOptions

// NewImageModel creates a new OpenAI image generation model
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
	return m.provider.Name() + ".image"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// openaiFileInputSupportedModels lists OpenAI image model IDs known to
// accept file inputs for image editing (TS
// OpenAIImageModel#supportsFileInputs).
var openaiFileInputSupportedModels = map[string]bool{
	"dall-e-2":                          true,
	"gpt-image-1":                       true,
	"gpt-image-1-mini":                  true,
	"gpt-image-1.5":                     true,
	"gpt-image-2":                       true,
	"gpt-image-2.5-flare":               true,
	"gpt-image-2.5-flare-2026-09-08":    true,
	"gpt-image-2.5-sunburst":            true,
	"gpt-image-2.5-sunburst-2026-09-08": true,
	"chatgpt-image-latest":              true,
}

// openaiModelMaxImagesPerCall mirrors TS modelMaxImagesPerCall.
var openaiModelMaxImagesPerCall = map[string]int{
	"dall-e-3":                          1,
	"dall-e-2":                          10,
	"gpt-image-1":                       10,
	"gpt-image-1-mini":                  10,
	"gpt-image-1.5":                     10,
	"gpt-image-2":                       10,
	"gpt-image-2.5-flare":               10,
	"gpt-image-2.5-flare-2026-09-08":    10,
	"gpt-image-2.5-sunburst":            10,
	"gpt-image-2.5-sunburst-2026-09-08": 10,
	"chatgpt-image-latest":              10,
}

// ImageMaxImagesPerCall returns the per-call image limit for an OpenAI image
// model ID (TS getMaxImagesPerCall): known IDs per the table, any other
// gpt-image-* ID 10, everything else 1.
func ImageMaxImagesPerCall(modelID string) int {
	if n, ok := openaiModelMaxImagesPerCall[modelID]; ok {
		return n
	}
	if strings.HasPrefix(modelID, "gpt-image-") {
		return 10
	}
	return 1
}

// MaxImagesPerCall returns the number of images the model can generate in a
// single call (TS OpenAIImageModel#maxImagesPerCall).
func (m *ImageModel) MaxImagesPerCall() int {
	return ImageMaxImagesPerCall(m.modelID)
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if openaiFileInputSupportedModels[m.modelID] {
		return boolPtr(true)
	}
	if m.modelID == "dall-e-3" {
		return boolPtr(false)
	}
	return nil
}

// SupportsMaskInputs mirrors SupportsFileInputs (TS returns the same value
// for both capabilities).
func (m *ImageModel) SupportsMaskInputs() *bool {
	return m.SupportsFileInputs()
}

func boolPtr(b bool) *bool {
	return &b
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	if m.provider.config.OpenAICompatible {
		return m.doCompat(ctx, opts)
	}
	if len(opts.Files) > 0 {
		if err := validateOpenAIImageProviderOptions(opts.ProviderOptions, true); err != nil {
			return nil, err
		}
		return m.doEdit(ctx, opts)
	}

	if err := validateOpenAIImageProviderOptions(opts.ProviderOptions, false); err != nil {
		return nil, err
	}
	reqBody := m.buildRequestBody(opts)
	var response openaiImageResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaOpenAIImage),
		Method:           http.MethodPost,
		Path:             "/images/generations",
		Query:            m.provider.config.ImageQuery,
		Body:             reqBody,
		Headers:          opts.Headers,
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = imageWarnings(opts)
	result.Response = &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: providerutils.ExtractHeaders(httpResp.Headers)}
	return result, nil
}

// defaultResponseFormatPrefixes lists model ID prefixes that have their own
// default response format and should not receive an explicit "response_format"
// override. Setting response_format: "b64_json" on these models causes errors
// because they use a different default format (#12838).
var defaultResponseFormatPrefixes = []string{
	"chatgpt-image-",
	"gpt-image-",
}

// hasDefaultResponseFormat returns true when the model has its own built-in
// response format and must not be sent an explicit response_format field.
func hasDefaultResponseFormat(modelID string) bool {
	for _, prefix := range defaultResponseFormatPrefixes {
		if len(modelID) >= len(prefix) && modelID[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	openaiOpts := extractOpenAIImageProviderOptions(opts.ProviderOptions)
	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	// Only set response_format for models that don't have a built-in default.
	// chatgpt-image and gpt-image-1 variants manage their own format (#12838).
	if !hasDefaultResponseFormat(m.modelID) {
		body["response_format"] = "b64_json"
	}
	if opts.N != nil {
		body["n"] = *opts.N
	}
	if size := firstNonEmpty(openaiOpts.Size, opts.Size); size != "" {
		body["size"] = size
	}
	if quality := firstNonEmpty(openaiOpts.Quality, opts.Quality); quality != "" {
		body["quality"] = quality
	}
	if style := firstNonEmpty(openaiOpts.Style, opts.Style); style != "" {
		body["style"] = style
	}
	if openaiOpts.Background != "" {
		body["background"] = openaiOpts.Background
	}
	if openaiOpts.Moderation != "" {
		body["moderation"] = openaiOpts.Moderation
	}
	if openaiOpts.OutputFormat != "" {
		body["output_format"] = openaiOpts.OutputFormat
	}
	if openaiOpts.OutputCompression != nil {
		body["output_compression"] = *openaiOpts.OutputCompression
	}
	if openaiOpts.User != "" {
		body["user"] = openaiOpts.User
	}
	return body
}

func (m *ImageModel) doEdit(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	body, contentType, err := m.buildEditMultipartBody(ctx, opts)
	if err != nil {
		return nil, err
	}

	var response openaiImageResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaOpenAIImage),
		Method:           http.MethodPost,
		Path:             "/images/edits",
		Query:            m.provider.config.ImageQuery,
		Headers:          internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
		Body:             body,
	}, &response)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = imageWarnings(opts)
	result.Response = &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: providerutils.ExtractHeaders(httpResp.Headers)}
	return result, nil
}

func (m *ImageModel) buildEditMultipartBody(ctx context.Context, opts *provider.ImageGenerateOptions) (io.Reader, string, error) {
	openaiOpts := extractOpenAIImageProviderOptions(opts.ProviderOptions)
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		err := func() error {
			if err := writer.WriteField("model", m.modelID); err != nil {
				return err
			}
			if opts.Prompt != "" {
				if err := writer.WriteField("prompt", opts.Prompt); err != nil {
					return err
				}
			}
			if opts.N != nil {
				if err := writer.WriteField("n", fmt.Sprintf("%d", *opts.N)); err != nil {
					return err
				}
			}
			if size := firstNonEmpty(openaiOpts.Size, opts.Size); size != "" {
				if err := writer.WriteField("size", size); err != nil {
					return err
				}
			}
			for key, value := range map[string]string{
				"quality":        openaiOpts.Quality,
				"background":     openaiOpts.Background,
				"output_format":  openaiOpts.OutputFormat,
				"input_fidelity": openaiOpts.InputFidelity,
				"user":           openaiOpts.User,
			} {
				if value != "" {
					if err := writer.WriteField(key, value); err != nil {
						return err
					}
				}
			}
			if openaiOpts.OutputCompression != nil {
				if err := writer.WriteField("output_compression", fmt.Sprintf("%d", *openaiOpts.OutputCompression)); err != nil {
					return err
				}
			}

			imageField := "image"
			if len(opts.Files) > 1 {
				imageField = "image[]"
			}
			for i, file := range opts.Files {
				if err := m.writeImageFilePart(ctx, writer, imageField, fmt.Sprintf("image-%d%s", i, imageExtension(file.MediaType)), file); err != nil {
					return err
				}
			}
			if opts.Mask != nil {
				if err := m.writeImageFilePart(ctx, writer, "mask", "mask"+imageExtension(opts.Mask.MediaType), *opts.Mask); err != nil {
					return err
				}
			}
			return writer.Close()
		}()
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	return pr, writer.FormDataContentType(), nil
}

func (m *ImageModel) writeImageFilePart(ctx context.Context, writer *multipart.Writer, fieldName, filename string, file provider.ImageFile) error {
	// TS appends a Blob typed with the file's media type; FormData names a
	// Blob "blob". Providers reject application/octet-stream image parts.
	part, err := providerutils.CreateTypedFormFile(writer, fieldName, "blob", file.MediaType)
	if err != nil {
		return err
	}
	if file.Type == "url" && file.URL != "" {
		// file.URL is caller-supplied data (an image-edit input or mask
		// URL), not the provider's own endpoint, so it must go through the
		// same validated, DNS-pinned download path as any other
		// response/caller-supplied URL (TS fileToBlob -> downloadBlob ->
		// fetchWithValidatedRedirects) instead of the provider's plain HTTP
		// client. Before this fix, an attacker-controlled URL here could
		// reach an internal address (e.g. a cloud metadata endpoint) with no
		// SSRF check, no DNS pinning and no size limit; its response body
		// would be forwarded to OpenAI as image data.
		data, err := fileutil.Download(ctx, file.URL, fileutil.DefaultDownloadOptions())
		if err != nil {
			return fmt.Errorf("failed to download image %s: %w", file.URL, err)
		}
		_, err = part.Write(data)
		return err
	}
	_, err = part.Write(file.Data)
	return err
}

func imageExtension(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func imageWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	warnings := []types.Warning{}
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "This model does not support aspect ratio. Use `size` instead.",
		})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	return warnings
}

func extractOpenAIImageProviderOptions(providerOptions map[string]interface{}) OpenAIImageModelOptions {
	if providerOptions == nil {
		return OpenAIImageModelOptions{}
	}
	raw, ok := providerOptions["openai"]
	if !ok {
		return OpenAIImageModelOptions{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return OpenAIImageModelOptions{}
	}
	var opts OpenAIImageModelOptions
	_ = json.Unmarshal(data, &opts)
	return opts
}

// openAIImageBaseOptionsSchema is baseImageModelOptionsObject in
// openai-image-model-options.ts; generation adds style/moderation and edit
// adds inputFidelity (unknown keys pass, like z.object).
func openAIImageOptionsSchema(edit bool) providerutils.OptionSchema {
	zero, hundred := 0, 100
	schema := providerutils.OptionSchema{
		"quality":           providerutils.OptEnum("standard", "hd", "low", "medium", "high", "xhigh", "max", "auto"),
		"background":        providerutils.OptEnum("transparent", "opaque", "auto"),
		"outputFormat":      providerutils.OptEnum("png", "jpeg", "webp"),
		"outputCompression": providerutils.OptInt(&zero, &hundred),
		"user":              providerutils.OptString,
	}
	if edit {
		schema["inputFidelity"] = providerutils.OptEnum("high", "low")
	} else {
		schema["style"] = providerutils.OptEnum("vivid", "natural")
		schema["moderation"] = providerutils.OptEnum("auto", "low")
	}
	return schema
}

// validateOpenAIImageProviderOptions is parseProviderOptions with the
// generation or edit schema: invalid options fail with an InvalidArgumentError.
func validateOpenAIImageProviderOptions(providerOptions map[string]interface{}, edit bool) error {
	raw := providerOptions["openai"]
	if raw == nil {
		return nil
	}
	// Typed option structs (OpenAIImageModelOptions) validate through their
	// JSON form too: an out-of-range value in a struct is as invalid as in a map.
	if encoded, err := json.Marshal(raw); err == nil {
		var normalized interface{}
		if json.Unmarshal(encoded, &normalized) == nil {
			raw = normalized
		}
	}
	_, err := providerutils.ParseProviderOptions("openai", map[string]interface{}{"openai": raw}, openAIImageOptionsSchema(edit))
	return err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (m *ImageModel) convertResponse(response openaiImageResponse) (*types.ImageResult, error) {
	if len(response.Data) == 0 {
		// TS returns an empty `images` array; core then retries and finally
		// throws NoImageGeneratedError, so this is not a provider error.
		empty := &types.ImageResult{
			MimeType:         "image/png",
			ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"images": []map[string]interface{}{}}},
		}
		if u := response.Usage; u != nil {
			empty.Usage.InputTokens = u.InputTokens
			empty.Usage.OutputTokens = u.OutputTokens
			empty.Usage.TotalTokens = u.TotalTokens
		}
		return empty, nil
	}
	images := make([][]byte, 0, len(response.Data))
	b64 := make([]string, 0, len(response.Data))
	for _, d := range response.Data {
		raw, err := base64.StdEncoding.DecodeString(d.B64JSON)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 image: %w", err)
		}
		images = append(images, raw)
		b64 = append(b64, d.B64JSON)
	}
	result := &types.ImageResult{
		Image:        images[0],
		Images:       images,
		Base64Image:  b64[0],
		Base64Images: b64,
		MimeType:     "image/png",
		URL:          response.Data[0].URL,
		Usage:        types.ImageUsage{ImageCount: len(response.Data)},
	}
	if u := response.Usage; u != nil {
		result.Usage.InputTokens = u.InputTokens
		result.Usage.OutputTokens = u.OutputTokens
		result.Usage.TotalTokens = u.TotalTokens
	}
	// TS providerMetadata.openai.images: per-image revisedPrompt plus response
	// fields, with input token details split evenly (remainder on the last).
	total := len(response.Data)
	imagesMeta := make([]map[string]interface{}, total)
	for i, d := range response.Data {
		item := map[string]interface{}{}
		if d.RevisedPrompt != "" {
			item["revisedPrompt"] = d.RevisedPrompt
		}
		if response.Created != 0 {
			item["created"] = response.Created
		}
		if response.Size != "" {
			item["size"] = response.Size
		}
		if response.Quality != "" {
			item["quality"] = response.Quality
		}
		if response.Background != "" {
			item["background"] = response.Background
		}
		if response.OutputFormat != "" {
			item["outputFormat"] = response.OutputFormat
		}
		if det := response.Usage; det != nil && det.InputTokensDetails != nil {
			if det.InputTokensDetails.ImageTokens != nil {
				item["imageTokens"] = distributeTokens(*det.InputTokensDetails.ImageTokens, i, total)
			}
			if det.InputTokensDetails.TextTokens != nil {
				item["textTokens"] = distributeTokens(*det.InputTokensDetails.TextTokens, i, total)
			}
		}
		imagesMeta[i] = item
	}
	result.ProviderMetadata = map[string]interface{}{"openai": map[string]interface{}{"images": imagesMeta}}
	return result, nil
}

func distributeTokens(totalTokens, index, count int) int {
	base := totalTokens / count
	if index == count-1 {
		return totalTokens - base*(count-1)
	}
	return base
}

type openaiImageResponse struct {
	Created      int64  `json:"created"`
	Size         string `json:"size"`
	Quality      string `json:"quality"`
	Background   string `json:"background"`
	OutputFormat string `json:"output_format"`
	Data         []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Usage *struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		InputTokensDetails *struct {
			ImageTokens *int `json:"image_tokens"`
			TextTokens  *int `json:"text_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}
