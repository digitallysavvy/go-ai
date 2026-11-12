package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ImageModel implements the provider.ImageModel interface for AI Gateway
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new AI Gateway image model
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
	return "gateway"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// MaxImagesPerCall returns the maximum number of images accepted in one request.
func (m *ImageModel) MaxImagesPerCall() int {
	// JavaScript Number.MAX_SAFE_INTEGER, clamped so 32-bit builds compile.
	return min(9007199254740991, math.MaxInt)
}

// DoGenerate generates an image based on the given options
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Build request body
	body := map[string]interface{}{}
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}

	// TS sends `prompt` unconditionally, but an image-only edit has an
	// undefined prompt that JSON.stringify drops. Go cannot tell "" from
	// unset, so an empty prompt accompanied by input files is treated as unset.
	if opts.Prompt != "" || len(opts.Files) == 0 {
		body["prompt"] = opts.Prompt
	}

	if opts.N != nil {
		body["n"] = *opts.N
	}

	if opts.Size != "" {
		body["size"] = opts.Size
	}

	if opts.AspectRatio != "" {
		body["aspectRatio"] = opts.AspectRatio
	}

	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// TS 89aff56165: experimental_streamingImageDecoding is a client option;
	// it is removed from the request (an empty gateway entry and an empty
	// providerOptions with it) without changing the caller's map.
	requestProviderOptions, streamingImageDecoding := gatewayStripStreamingImageDecoding(opts.ProviderOptions)
	if requestProviderOptions != nil {
		body["providerOptions"] = requestProviderOptions
	}

	if opts.Files != nil {
		files := make([]map[string]interface{}, 0, len(opts.Files))
		for _, file := range opts.Files {
			files = append(files, gatewayImageFile(file))
		}
		body["files"] = files
	}

	if opts.Mask != nil {
		body["mask"] = gatewayImageFile(*opts.Mask)
	}

	// TS combineHeaders(config, options.headers, model headers, o11y)
	headers := gatewayRequestHeaders(ctx, opts.Headers, m.getModelConfigHeaders())

	request := internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator("gateway", providerutils.SchemaGatewayImage),
		Method:           http.MethodPost,
		Path:             "/image-model",
		Body:             body,
		Headers:          headers,
	}
	if streamingImageDecoding {
		response, binaries, respHeaders, err := m.doStreamingImageRequest(ctx, request)
		if err != nil {
			return nil, m.handleErrorWithContext(ctx, err)
		}
		return m.convertStreamingResponse(response, binaries, respHeaders), nil
	}

	// Make API request
	var response gatewayImageResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, request, &response)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	return m.convertResponse(response, httpResp.Headers), nil
}

// gatewayStreamingImageDecodingKey is the client-only Gateway image option
// (TS gateway-provider-options.ts experimental_streamingImageDecoding).
const gatewayStreamingImageDecodingKey = "experimental_streamingImageDecoding"

// gatewayStripStreamingImageDecoding is TS gateway-image-model.ts 74-94: it
// reports whether providerOptions.gateway.experimental_streamingImageDecoding
// is true and returns the provider options to send, without that key; a
// gateway entry left empty is removed, and so is a providerOptions left empty
// (nil: not sent). The caller's maps are not modified.
func gatewayStripStreamingImageDecoding(providerOptions map[string]interface{}) (map[string]interface{}, bool) {
	raw, ok := providerOptions["gateway"]
	if !ok || raw == nil {
		return providerOptions, false
	}
	var gatewayOptions map[string]interface{}
	switch v := raw.(type) {
	case map[string]interface{}:
		gatewayOptions = v
	case GatewayProviderOptions:
		gatewayOptions = v.toMap()
	case *GatewayProviderOptions:
		if v == nil {
			return providerOptions, false
		}
		gatewayOptions = v.toMap()
	default:
		normalized, _ := providerutils.NormalizeOptionsJSON(map[string]interface{}{"gateway": raw})["gateway"].(map[string]interface{})
		gatewayOptions = normalized
	}
	value, present := gatewayOptions[gatewayStreamingImageDecodingKey]
	if !present {
		return providerOptions, false
	}
	enabled, _ := value.(bool)
	serviceOptions := make(map[string]interface{}, len(gatewayOptions))
	for k, v := range gatewayOptions {
		if k != gatewayStreamingImageDecodingKey {
			serviceOptions[k] = v
		}
	}
	out := make(map[string]interface{}, len(providerOptions))
	for k, v := range providerOptions {
		out[k] = v
	}
	if len(serviceOptions) > 0 {
		out["gateway"] = serviceOptions
	} else {
		delete(out, "gateway")
	}
	if len(out) == 0 {
		return nil, enabled
	}
	return out, enabled
}

// doStreamingImageRequest is TS createJsonResponseHandler with
// experimental_binaryFields ['images[*]'] (89aff56165): the body is read in
// chunks under the response size limit, canonical base64 images of at least
// 64 KiB are decoded while they arrive, and the skeleton (with placeholders
// for them) is validated and parsed. A validation or parse error carries the
// skeleton, never the image bytes.
func (m *ImageModel) doStreamingImageRequest(ctx context.Context, request internalhttp.Request) (gatewayImageResponse, map[string][]byte, http.Header, error) {
	var response gatewayImageResponse
	httpResp, err := m.provider.client.DoStream(ctx, request)
	if err != nil {
		return response, nil, nil, err
	}
	defer httpResp.Body.Close() //nolint:errcheck

	paths, err := compileBinaryFieldPaths([]string{"images[*]"})
	if err != nil {
		return response, nil, nil, err
	}
	scanner := newJSONBinaryFieldScanner(paths, binaryFieldMinBytes)
	url := m.provider.baseURL + request.Path
	if err := gatewayReadResponseChunks(httpResp, url, fileutil.DefaultMaxDownloadSize, scanner.push); err != nil {
		return response, nil, nil, err
	}
	text, binaries := scanner.finish()
	skeleton := []byte(text)
	if err := request.ValidateResponse(httpResp.StatusCode, httpResp.Header, skeleton); err != nil {
		return response, nil, nil, err
	}
	if err := json.Unmarshal(skeleton, &response); err != nil {
		// Like internalhttp's decodeJSONBody: a z.number() count sent as
		// 1.0 or 1e3 is truncated instead of failing the Go int field.
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) || !strings.HasPrefix(typeErr.Value, "number") || providerutils.UnmarshalChunk(skeleton, &response) != nil {
			return response, nil, nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
	}
	return response, binaries, httpResp.Header, nil
}

// gatewayReadResponseChunks is TS readResponseWithSizeLimit with onChunk: the
// Content-Length check first, then every chunk is handed to onChunk instead of
// being collected; a read error or a body over maxBytes is a DownloadError.
func gatewayReadResponseChunks(resp *http.Response, url string, maxBytes int64, onChunk func([]byte)) error {
	statusText := http.StatusText(resp.StatusCode)
	if prefix := fmt.Sprintf("%d ", resp.StatusCode); strings.HasPrefix(resp.Status, prefix) {
		statusText = strings.TrimPrefix(resp.Status, prefix)
	}
	if raw := strings.TrimSpace(resp.Header.Get("Content-Length")); raw != "" {
		if contentLength, err := strconv.ParseInt(raw, 10, 64); err == nil && contentLength > maxBytes {
			return providererrors.NewDownloadError(url, resp.StatusCode, statusText,
				fmt.Sprintf("Download of %s exceeded maximum size of %d bytes (Content-Length: %d).", url, maxBytes, contentLength), nil)
		}
	}
	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return providererrors.NewDownloadError(url, resp.StatusCode, statusText,
					fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, maxBytes), nil)
			}
			onChunk(buf[:n])
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return providererrors.NewDownloadError(url, resp.StatusCode, statusText, "", err)
		}
	}
}

// convertStreamingResponse is convertResponse for a streaming-decoded body.
// TS toImageModelImages: a result holds either strings or bytes; when any
// image was decoded while reading, every image is returned as bytes (Images)
// and Base64Images stays empty; otherwise the result is the regular one.
func (m *ImageModel) convertStreamingResponse(response gatewayImageResponse, binaries map[string][]byte, headers http.Header) *types.ImageResult {
	extracted := false
	for _, image := range response.Images {
		if _, ok := binaries[image]; ok {
			extracted = true
			break
		}
	}
	result := m.convertResponse(response, headers)
	if !extracted {
		return result
	}
	for i, image := range response.Images {
		if data, ok := binaries[image]; ok {
			result.Images[i] = data
		}
	}
	result.Base64Images = nil
	result.Base64Image = ""
	if len(result.Images) > 0 {
		result.Image = result.Images[0]
	}
	return result
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *ImageModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-image-model-specification-version": "4",
		"ai-model-id":                          m.modelID,
	}
}

type gatewayImageResponse struct {
	Images           []string               `json:"images"`
	Warnings         []types.Warning        `json:"warnings,omitempty"`
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`
	// IsRetryable mirrors TS ImageModelV4Result.isRetryable (gateway-image-model.ts,
	// added by aa4cc14): the Gateway can report that an empty/failed image
	// result is safe to retry. Only set when the server includes it.
	//
	// Set on both types.ImageResult.IsRetryable (the core surface read by
	// pkg/ai/generate_image.go to decide whether to retry an empty-image
	// result, mirroring TS generate-image.ts's `result.isRetryable !== false`
	// check) and mirrored into ProviderMetadata["gateway"]["isRetryable"] for
	// back-compat with callers already reading it from there.
	IsRetryable *bool `json:"isRetryable,omitempty"`
	Usage       *struct {
		InputTokens  *int `json:"inputTokens"`
		OutputTokens *int `json:"outputTokens"`
		TotalTokens  *int `json:"totalTokens"`
	} `json:"usage,omitempty"`
}

func (m *ImageModel) convertResponse(response gatewayImageResponse, headers http.Header) *types.ImageResult {
	images := make([][]byte, 0, len(response.Images))
	for _, image := range response.Images {
		// The wire format is always base64; Images carries the decoded bytes
		// (Base64Images keeps the strings). Undecodable payloads are passed
		// through unchanged rather than dropped.
		if decoded, err := types.DecodeFileDataString(image); err == nil {
			images = append(images, decoded)
		} else {
			images = append(images, []byte(image))
		}
	}
	warnings := warningsOrEmpty(response.Warnings)
	providerMetadata := response.ProviderMetadata
	if response.IsRetryable != nil {
		providerMetadata = cloneMapStringInterface(providerMetadata)
		if providerMetadata == nil {
			providerMetadata = map[string]interface{}{}
		}
		// Merge into (rather than replace) any existing "gateway" provider
		// metadata namespace, which already carries server-reported
		// per-image metadata (e.g. "images").
		gatewayMeta := map[string]interface{}{}
		if existing, ok := providerMetadata["gateway"].(map[string]interface{}); ok {
			gatewayMeta = cloneMapStringInterface(existing)
		}
		gatewayMeta["isRetryable"] = *response.IsRetryable
		providerMetadata["gateway"] = gatewayMeta
	}
	result := &types.ImageResult{
		Images:           images,
		Base64Images:     response.Images,
		MimeType:         "image/png",
		Warnings:         warnings,
		ProviderMetadata: providerMetadata,
		Response:         &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: flattenHeaders(headers)},
		Usage:            types.ImageUsage{ImageCount: len(response.Images)},
		IsRetryable:      response.IsRetryable,
	}
	if len(images) > 0 {
		result.Image = images[0]
		result.Base64Image = response.Images[0]
	}
	if response.Usage != nil {
		if response.Usage.InputTokens != nil {
			result.Usage.InputTokens = *response.Usage.InputTokens
		}
		if response.Usage.OutputTokens != nil {
			result.Usage.OutputTokens = *response.Usage.OutputTokens
		}
		if response.Usage.TotalTokens != nil {
			result.Usage.TotalTokens = *response.Usage.TotalTokens
		}
	}
	return result
}

func gatewayImageFile(file provider.ImageFile) map[string]interface{} {
	result := map[string]interface{}{}
	if file.Type != "" {
		result["type"] = file.Type
	}
	if file.URL != "" {
		result["url"] = file.URL
	}
	if file.Data != nil {
		result["data"] = base64.StdEncoding.EncodeToString(file.Data)
	}
	if file.MediaType != "" {
		result["mediaType"] = file.MediaType
	}
	if len(file.ProviderOptions) > 0 {
		result["providerOptions"] = file.ProviderOptions
	}
	return result
}

// flattenHeaders is TS extractResponseHeaders: a repeated header joins its
// values with ", " (fetch Headers semantics), never just the first value.
func flattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	return providerutils.ExtractHeaders(headers)
}

// handleError converts errors to appropriate provider errors
func (m *ImageModel) handleError(err error) error {
	return m.handleErrorWithContext(context.Background(), err)
}

func (m *ImageModel) handleErrorWithContext(ctx context.Context, err error) error {
	return m.provider.handleErrorWithContext(ctx, err)
}
