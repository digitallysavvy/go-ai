package deepinfra

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ImageModel implements the provider.ImageModel interface for DeepInfra.
//
// DeepInfra's image generation API is its own protocol (POST
// {baseURL}/{modelId} with num_images/aspect_ratio/width/height), not
// OpenAI-compatible; image editing uses a separate OpenAI-compatible
// /images/edits multipart endpoint. Mirrors TS DeepInfraImageModel.
type ImageModel struct {
	provider    *Provider
	modelID     string
	baseURL     string
	editBaseURL string
}

// NewImageModel creates a new DeepInfra image generation model.
func NewImageModel(p *Provider, modelID, baseURL, editBaseURL string) *ImageModel {
	return &ImageModel{provider: p, modelID: modelID, baseURL: baseURL, editBaseURL: editBaseURL}
}

// SpecificationVersion returns the specification version.
func (m *ImageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *ImageModel) Provider() string { return "deepinfra.image" }

// ModelID returns the model ID.
func (m *ImageModel) ModelID() string { return m.modelID }

// MaxImagesPerCall returns the maximum number of images per call.
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// deepInfraFileInputModels lists the known DeepInfra model IDs that accept
// file inputs for image editing (TS DeepInfraImageModel#supportsFileInputs).
var deepInfraFileInputModels = map[string]bool{
	"stabilityai/sd3.5":                    true,
	"black-forest-labs/FLUX-1.1-pro":       true,
	"black-forest-labs/FLUX-1-schnell":     true,
	"black-forest-labs/FLUX-1-dev":         true,
	"black-forest-labs/FLUX-pro":           true,
	"black-forest-labs/FLUX.1-Kontext-dev": true,
	"black-forest-labs/FLUX.1-Kontext-pro": true,
	"Qwen/Qwen-Image-Edit":                 true,
	"stabilityai/sd3.5-medium":             true,
	"stabilityai/sdxl-turbo":               true,
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if deepInfraFileInputModels[m.modelID] {
		return boolPtr(true)
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

// DoGenerate performs image generation or, when Files are supplied, image
// editing via DeepInfra's OpenAI-compatible /images/edits endpoint.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if err := providerutils.ValidateMapProviderOptions("deepinfra", opts.ProviderOptions, deepInfraImageOptionsSchema); err != nil {
		return nil, err
	}
	if len(opts.Files) > 0 {
		return m.doEdit(ctx, opts)
	}
	return m.doGenerate(ctx, opts)
}

func (m *ImageModel) doGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	body := map[string]interface{}{
		"prompt": opts.Prompt,
	}
	if opts.N != nil {
		body["num_images"] = *opts.N
	}
	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}
	// Some DeepInfra models support size while others support aspect ratio;
	// allow passing either and leave validation to the server.
	// TS `...(splitSize && {width: splitSize[0], height: splitSize[1]})`
	// with size.split('x'): a size without "x" sends only width, and parts
	// after the second are ignored.
	if opts.Size != "" {
		parts := strings.Split(opts.Size, "x")
		body["width"] = parts[0]
		if len(parts) > 1 {
			body["height"] = parts[1]
		}
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	for key, value := range deepInfraImageOptions(opts.ProviderOptions) {
		body[key] = value
	}

	client := m.provider.imageClient(m.baseURL)
	resp, err := client.Do(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/" + m.modelID,
		// TS JSON.stringify({prompt, num_images, aspect_ratio, width, height,
		// seed, ...options}): that key order, not sorted keys.
		Body:    bytes.NewBufferString(providerutils.StringifyOrdered(body, map[string][]string{"": {"prompt", "num_images", "aspect_ratio", "width", "height", "seed"}})),
		Headers: opts.Headers,
	})
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
		}
		if noResp, ok := internalhttp.NoResponseError(err); ok {
			return nil, noResp // TS handleFetchError: an abort or timeout is returned unchanged
		}
		if providererrors.IsLoadAPIKeyError(err) {
			return nil, err // TS throws LoadAPIKeyError unwrapped (loadApiKey in the headers)
		}
		return nil, providererrors.NewProviderError("deepinfra", 0, "", err.Error(), err)
	}
	// TS response.text() / json(): a BOM is dropped and invalid UTF-8
	// replaced (WHATWG TextDecoder) before the body is parsed.
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if resp.StatusCode >= 400 {
		return nil, m.parseGenerateError(resp)
	}
	if err := providerutils.MediaResponseValidator("deepinfra", providerutils.SchemaDeepInfraImage)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var decoded struct {
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		return nil, fmt.Errorf("deepinfra: failed to decode image response: %w", err)
	}

	result := &types.ImageResult{Usage: types.ImageUsage{ImageCount: len(decoded.Images)}}
	for _, image := range decoded.Images {
		b64 := stripDataURLPrefix(image)
		result.Base64Images = append(result.Base64Images, b64)
		if decodedBytes, err := base64.StdEncoding.DecodeString(b64); err == nil {
			result.Images = append(result.Images, decodedBytes)
			if result.Image == nil {
				result.Image = decodedBytes
				result.Base64Image = b64
			}
		}
	}
	result.MimeType = "image/png"
	result.Response = &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: providerutils.ExtractHeaders(resp.Headers)}
	return result, nil
}

var dataURLPrefix = regexp.MustCompile(`^data:image/\w+;base64,`)

func stripDataURLPrefix(image string) string {
	return dataURLPrefix.ReplaceAllString(image, "")
}

// parseGenerateError is TS createJsonErrorResponseHandler with
// deepInfraErrorSchema ({detail: {error: string}}): a blank body or one that
// does not match the schema gives the status text and no data; a matching
// body gives detail.error, even "", and the schema output as data
// ({detail: {error}}, undeclared keys stripped; tools-08).
func (m *ImageModel) parseGenerateError(resp *internalhttp.Response) error {
	return deepInfraSchemaError(resp, deepInfraGenerateErrorSchema)
}

func deepInfraGenerateErrorSchema(body interface{}) (string, interface{}, bool) {
	obj, ok := body.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	detail, ok := obj["detail"].(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	message, ok := detail["error"].(string)
	if !ok {
		return "", nil, false
	}
	return message, map[string]interface{}{"detail": map[string]interface{}{"error": message}}, true
}

// doEdit performs image editing via DeepInfra's OpenAI-compatible
// /images/edits multipart endpoint (baseURL with /inference replaced by
// /openai, mirroring TS getEditUrl).
func (m *ImageModel) doEdit(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	body, contentType, err := m.buildEditMultipartBody(ctx, opts)
	if err != nil {
		return nil, err
	}

	client := m.provider.imageClient(m.editBaseURL)
	resp, err := client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/images/edits",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	})
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
		}
		if noResp, ok := internalhttp.NoResponseError(err); ok {
			return nil, noResp // TS handleFetchError: an abort or timeout is returned unchanged
		}
		if providererrors.IsLoadAPIKeyError(err) {
			return nil, err // TS throws LoadAPIKeyError unwrapped (loadApiKey in the headers)
		}
		return nil, providererrors.NewProviderError("deepinfra", 0, "", err.Error(), err)
	}
	// TS response.text() / json(): a BOM is dropped and invalid UTF-8
	// replaced (WHATWG TextDecoder) before the body is parsed.
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if resp.StatusCode >= 400 {
		return nil, m.parseEditError(resp)
	}
	if err := providerutils.MediaResponseValidator("deepinfra", providerutils.SchemaDeepInfraEdit)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var decoded struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		return nil, fmt.Errorf("deepinfra: failed to decode image edit response: %w", err)
	}

	result := &types.ImageResult{Usage: types.ImageUsage{ImageCount: len(decoded.Data)}}
	for _, item := range decoded.Data {
		result.Base64Images = append(result.Base64Images, item.B64JSON)
		if decodedBytes, err := base64.StdEncoding.DecodeString(item.B64JSON); err == nil {
			result.Images = append(result.Images, decodedBytes)
			if result.Image == nil {
				result.Image = decodedBytes
				result.Base64Image = item.B64JSON
			}
		}
	}
	result.MimeType = "image/png"
	result.Response = &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: providerutils.ExtractHeaders(resp.Headers)}
	return result, nil
}

// parseEditError is TS createJsonErrorResponseHandler with
// deepInfraEditErrorSchema ({error?: {message: string}}) and
// `error.error?.message ?? 'Unknown error'`: a blank body or one that does
// not match the schema (error null or not an object, message not a string)
// gives the status text and no data; a matching body without error gives
// "Unknown error" and data {}, one with error gives its message and data
// {error: {message}} (tools-08).
func (m *ImageModel) parseEditError(resp *internalhttp.Response) error {
	return deepInfraSchemaError(resp, deepInfraEditErrorSchema)
}

func deepInfraEditErrorSchema(body interface{}) (string, interface{}, bool) {
	obj, ok := body.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	raw, present := obj["error"]
	if !present {
		return "Unknown error", map[string]interface{}{}, true
	}
	errObj, ok := raw.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	message, ok := errObj["message"].(string)
	if !ok {
		return "", nil, false
	}
	return message, map[string]interface{}{"error": map[string]interface{}{"message": message}}, true
}

// deepInfraSchemaError is the APICallError of createJsonErrorResponseHandler:
// status, response headers (so retry-after(-ms) reaches the retry loop) and
// body, with the schema output as data only when the body matched.
func deepInfraSchemaError(resp *internalhttp.Response, schema providererrors.ErrorSchema) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}
	return providererrors.NewSchemaProviderError("deepinfra", cause, schema)
}

func (m *ImageModel) buildEditMultipartBody(ctx context.Context, opts *provider.ImageGenerateOptions) (io.Reader, string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		err := func() error {
			// TS convertToFormData({model, prompt, image, mask, n, size,
			// ...deepInfraOptions}, {useArrayBrackets: false}): fields in
			// that order, an option with one of those names replaces it in
			// place, null/undefined values are skipped, and an array is
			// appended as repeated fields. Other option keys follow, sorted
			// (a Go map keeps no insertion order).
			options := deepInfraImageOptions(opts.ProviderOptions)
			fixed := []string{"model", "prompt", "image", "mask", "n", "size"}
			for _, key := range fixed {
				if value, overridden := options[key]; overridden {
					if err := writeDeepInfraFormValue(writer, key, value); err != nil {
						return err
					}
					continue
				}
				var err error
				switch key {
				case "model":
					err = providerutils.WriteFormDataField(writer, "model", m.modelID)
				case "prompt":
					if opts.Prompt != "" {
						err = providerutils.WriteFormDataField(writer, "prompt", opts.Prompt)
					}
				case "image":
					for _, file := range opts.Files {
						if err = writeImageFilePart(ctx, writer, "image", "", file); err != nil {
							break
						}
					}
				case "mask":
					if opts.Mask != nil {
						err = writeImageFilePart(ctx, writer, "mask", "", *opts.Mask)
					}
				case "n":
					if opts.N != nil {
						err = providerutils.WriteFormDataField(writer, "n", strconv.Itoa(*opts.N))
					}
				case "size":
					if opts.Size != "" {
						err = providerutils.WriteFormDataField(writer, "size", opts.Size)
					}
				}
				if err != nil {
					return err
				}
			}
			rest := make([]string, 0, len(options))
			for key := range options {
				if !slices.Contains(fixed, key) {
					rest = append(rest, key)
				}
			}
			sort.Strings(rest)
			for _, key := range rest {
				if err := writeDeepInfraFormValue(writer, key, options[key]); err != nil {
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

// writeDeepInfraFormValue appends a provider option like TS
// convertToFormData: null is skipped, an array is appended element by
// element under the same name, and other values use JS String(value).
func writeDeepInfraFormValue(writer *multipart.Writer, key string, value interface{}) error {
	switch v := value.(type) {
	case nil:
		return nil
	case []interface{}:
		for _, item := range v {
			if err := providerutils.WriteFormDataField(writer, key, deepInfraFormString(item)); err != nil {
				return err
			}
		}
		return nil
	case []string:
		for _, item := range v {
			if err := providerutils.WriteFormDataField(writer, key, item); err != nil {
				return err
			}
		}
		return nil
	}
	return providerutils.WriteFormDataField(writer, key, deepInfraFormString(value))
}

func deepInfraFormString(value interface{}) string {
	switch v := value.(type) {
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float32:
		return providerutils.JSNumberString(float64(v))
	}
	return providerutils.JSString(value)
}

func writeImageFilePart(ctx context.Context, writer *multipart.Writer, fieldName, filename string, file provider.ImageFile) error {
	data, mediaType := file.Data, file.MediaType
	if file.Type == "url" {
		// TS fileToBlob: downloadBlob(file.url) types the Blob with the
		// response content-type (none without one), not file.mediaType.
		res, err := fileutil.DownloadWithMetadata(ctx, file.URL, fileutil.NoTimeoutDownloadOptions())
		if err != nil {
			return err
		}
		data, mediaType = res.Data, res.ContentType
	}
	// TS appends a typed Blob (filename "blob").
	part, err := providerutils.CreateTypedFormFile(writer, fieldName, "blob", mediaType)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

// deepInfraImageOptionsSchema is deepInfraImageModelOptionsSchema (a loose
// object: unknown keys pass; null is accepted for every declared key).
var deepInfraImageOptionsSchema = providerutils.OptionSchema{
	"negative_prompt":     providerutils.OptNullable(providerutils.OptString),
	"num_inference_steps": providerutils.OptNullable(providerutils.OptNumber),
	"guidance_scale":      providerutils.OptNullable(providerutils.OptNumber),
	"guidance":            providerutils.OptNullable(providerutils.OptNumber),
	"response_format":     providerutils.OptNullable(providerutils.OptEnum("b64_json")),
	"quality":             providerutils.OptNullable(providerutils.OptString),
	"style":               providerutils.OptNullable(providerutils.OptString),
	"user":                providerutils.OptNullable(providerutils.OptString),
}

// deepInfraImageOptions returns providerOptions.deepinfra as-is (a loose,
// already-snake_case map merged directly into the request body/form fields),
// mirroring TS's z.looseObject deepInfraImageModelOptionsSchema: fields like
// negative_prompt, num_inference_steps, guidance_scale, guidance,
// response_format, quality, style, and user pass through untransformed, and
// any other key the caller sets is forwarded too.
func deepInfraImageOptions(providerOptions map[string]interface{}) map[string]interface{} {
	// Go-typed literals read like their JSON form (OVC-01).
	opts, _ := providerutils.NormalizeOptionsJSON(providerOptions)["deepinfra"].(map[string]interface{})
	return opts
}
