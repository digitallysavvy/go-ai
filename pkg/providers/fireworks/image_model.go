package fireworks

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ImageModel implements the provider.ImageModel interface for Fireworks AI
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Fireworks AI image generation model
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
	return "fireworks.image"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// fireworksFileInputUnsupportedModels lists Fireworks model IDs known to be
// text-to-image only (TS FireworksImageModel#supportsFileInputs).
var fireworksFileInputUnsupportedModels = map[string]bool{
	"accounts/fireworks/models/flux-1-dev-fp8":                   true,
	"accounts/fireworks/models/flux-1-schnell-fp8":               true,
	"accounts/fireworks/models/playground-v2-5-1024px-aesthetic": true,
	"accounts/fireworks/models/japanese-stable-diffusion-xl":     true,
	"accounts/fireworks/models/playground-v2-1024px-aesthetic":   true,
	"accounts/fireworks/models/SSD-1B":                           true,
	"accounts/fireworks/models/stable-diffusion-xl-1024-v1-0":    true,
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if m.modelID == "accounts/fireworks/models/flux-kontext-pro" ||
		m.modelID == "accounts/fireworks/models/flux-kontext-max" {
		return boolPtr(true)
	}
	if fireworksFileInputUnsupportedModels[m.modelID] {
		return boolPtr(false)
	}
	return nil
}

// SupportsMaskInputs reports whether the model accepts a mask input for
// inpainting. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if m.SupportsFileInputs() == nil {
		return nil
	}
	return boolPtr(false)
}

func boolPtr(b bool) *bool {
	return &b
}

// DoGenerate performs image generation, routing flux-kontext-* models through
// the async submission and polling path.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if err := validateImageOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	if m.isAsyncModel() {
		return m.doGenerateAsync(ctx, opts)
	}
	return m.doGenerateSync(ctx, opts)
}

// isAsyncModel returns true for flux-kontext-* models that require async polling.
func (m *ImageModel) isAsyncModel() bool {
	return strings.Contains(m.modelID, "flux-kontext")
}

// fireworksSyncBackend mirrors TS modelToBackendConfig for the non-workflow
// models: URL format and whether the backend takes `size`.
var fireworksImageGenerationModels = map[string]bool{
	"accounts/fireworks/models/playground-v2-5-1024px-aesthetic": true,
	"accounts/fireworks/models/japanese-stable-diffusion-xl":     true,
	"accounts/fireworks/models/playground-v2-1024px-aesthetic":   true,
	"accounts/fireworks/models/stable-diffusion-xl-1024-v1-0":    true,
	"accounts/fireworks/models/SSD-1B":                           true,
}

// syncPath mirrors TS getUrlForModel for the synchronous (binary response)
// backends.
func (m *ImageModel) syncPath() string {
	if fireworksImageGenerationModels[m.modelID] {
		return "/v1/image_generation/" + m.modelID
	}
	return "/v1/workflows/" + m.modelID + "/text_to_image"
}

// MaxImagesPerCall mirrors TS FireworksImageModel#maxImagesPerCall.
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// doGenerateSync handles synchronous image generation: the backend answers
// with the image bytes directly (TS createBinaryResponseHandler).
func (m *ImageModel) doGenerateSync(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	warnings := m.buildWarnings(opts)
	currentDate := time.Now()
	reqBody := m.buildRequestBody(opts)

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{Method: "POST", Path: m.syncPath(), Body: reqBody, Headers: opts.Headers})
	if err != nil {
		return nil, providererrors.NewProviderError("fireworks", 0, "", err.Error(), err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, internalhttp.StatusProviderError("fireworks", resp.StatusCode, resp.Headers, resp.Body)
	}

	// TS createBinaryResponseHandler: an empty body is an error.
	if len(resp.Body) == 0 {
		return nil, providererrors.NewProviderError("fireworks", resp.StatusCode, "", "Response body is empty", nil)
	}

	mimeType := "image/png"
	if ct := resp.Headers.Get("Content-Type"); strings.HasPrefix(ct, "image/") {
		mimeType = strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
	}
	return &types.ImageResult{
		Image:    resp.Body,
		Images:   [][]byte{resp.Body},
		MimeType: mimeType,
		Usage:    types.ImageUsage{ImageCount: 1},
		Warnings: warnings,
		Response: &types.ResponseMetadata{Timestamp: currentDate, ModelID: m.modelID, Headers: providerutils.ExtractHeaders(resp.Headers)},
	}, nil
}

// doGenerateAsync handles async image generation for flux-kontext-* models.
// It submits a request, then polls until the image is ready.
func (m *ImageModel) doGenerateAsync(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	warnings := m.buildWarnings(opts)
	currentDate := time.Now()

	requestID, err := m.submitAsyncRequest(ctx, opts)
	if err != nil {
		return nil, err
	}

	result, err := m.pollAsyncResult(ctx, requestID, opts.Headers)
	if err != nil {
		return nil, err
	}

	result.Warnings = warnings
	if result.Response == nil {
		result.Response = &types.ResponseMetadata{}
	}
	result.Response.Timestamp = currentDate
	result.Response.ModelID = m.modelID
	return result, nil
}

// submitAsyncRequest POSTs to the async submit endpoint and returns the request_id.
func (m *ImageModel) submitAsyncRequest(ctx context.Context, opts *provider.ImageGenerateOptions) (string, error) {
	body := m.buildAsyncRequestBody(opts)

	var submitResp AsyncSubmitResponse
	if err := m.provider.client.DoJSON(ctx, internalhttp.Request{Method: "POST", Path: "/v1/workflows/" + m.modelID, Body: body, Headers: opts.Headers, ValidateResponse: providerutils.NodeResponseValidator("fireworks", asyncSubmitNode)}, &submitResp); err != nil {
		if providerutils.IsInvalidJSONResponse(err) {
			return "", err
		}
		return "", fmt.Errorf("Fireworks async submit failed: %w", err) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
	}

	if submitResp.RequestID == "" {
		return "", fmt.Errorf("Fireworks async submit returned empty request_id") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
	}

	return submitResp.RequestID, nil
}

// checkAsyncStatus polls the status of an async generation job once.
// Returns the image URL when succeeded, whether the job is complete, and any error.
func (m *ImageModel) checkAsyncStatus(ctx context.Context, requestID string, callHeaders map[string]string) (string, bool, error) {
	pollBody := map[string]interface{}{"id": requestID}

	var pollResp AsyncPollResponse
	if err := m.provider.client.DoJSON(ctx, internalhttp.Request{Method: "POST", Path: "/v1/workflows/" + m.modelID + "/get_result", Body: pollBody, Headers: callHeaders, ValidateResponse: providerutils.NodeResponseValidator("fireworks", asyncPollNode)}, &pollResp); err != nil {
		if providerutils.IsInvalidJSONResponse(err) {
			return "", false, err
		}
		return "", false, fmt.Errorf("Fireworks async status check failed: %w", err) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
	}

	switch pollResp.Status {
	case "Ready":
		if pollResp.Result == nil || pollResp.Result.Sample == nil {
			return "", false, fmt.Errorf("Fireworks poll response is Ready but missing result.sample") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
		}
		return *pollResp.Result.Sample, true, nil
	case "Error", "Failed":
		return "", false, fmt.Errorf("Fireworks image generation failed with status: %s", pollResp.Status) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
	default:
		// Pending, Running, or any unknown status → continue polling
		return "", false, nil
	}
}

// asyncPollIntervalMs returns the configured poll interval or the default (500ms).
func (m *ImageModel) asyncPollIntervalMs() int {
	if m.provider.config.ImagePollIntervalMs > 0 {
		return m.provider.config.ImagePollIntervalMs
	}
	return 500
}

// asyncPollTimeoutMs returns the configured poll timeout or the default (120000ms).
func (m *ImageModel) asyncPollTimeoutMs() int {
	if m.provider.config.ImagePollTimeoutMs > 0 {
		return m.provider.config.ImagePollTimeoutMs
	}
	return 120000
}

// pollAsyncResult polls for the async result until Ready, timeout, or context cancellation.
// Polls immediately on the first attempt, then waits between subsequent attempts (matching
// the TypeScript reference implementation).
func (m *ImageModel) pollAsyncResult(ctx context.Context, requestID string, callHeaders map[string]string) (*types.ImageResult, error) {
	intervalMs := m.asyncPollIntervalMs()
	timeoutMs := m.asyncPollTimeoutMs()

	// A wall-clock deadline (TS pollForImageUrl's AbortController + setTimeout),
	// not a fixed attempt count: a slow poll request (or a slow provider) could
	// otherwise let the loop run well past timeoutMs before it notices, since
	// counting attempts only bounds wall time when every request completes
	// instantly. Deriving pollCtx from the caller's ctx and canceling it both
	// on timeout and on return also aborts any poll request in flight (the
	// final image download deliberately uses the caller's own ctx instead;
	// see the comment at its call site below).
	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()

	for {
		imageURL, done, err := m.checkAsyncStatus(pollCtx, requestID, callHeaders)
		if err != nil {
			if timedOut.Load() {
				return nil, fmt.Errorf("Fireworks image generation timed out after %dms", timeoutMs) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
			}
			return nil, err
		}
		if done {
			// TS doGenerateAsync downloads with the caller's own abortSignal,
			// not the internal polling-deadline signal: once pollForImageUrl
			// resolves, the poll timeout no longer applies. Using pollCtx here
			// would let the deadline timer (still armed until this function
			// returns) abort an in-progress download that outlives the
			// remaining poll budget.
			return m.downloadImage(ctx, imageURL, callHeaders)
		}

		select {
		case <-pollCtx.Done():
			if timedOut.Load() {
				return nil, fmt.Errorf("Fireworks image generation timed out after %dms", timeoutMs) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
			}
			return nil, pollCtx.Err()
		case <-time.After(time.Duration(intervalMs) * time.Millisecond):
		}
	}
}

// downloadImage fetches the image binary from the given URL and returns an ImageResult
// with the raw bytes and MIME type, matching the TypeScript reference implementation.
//
// imageURL comes from the provider response body, so (TS getFromApi with
// validateUrl, credentialedOrigin and trustedOrigin set to the base URL):
// foreign-origin URLs and redirect hops are SSRF-validated and DNS-pinned, and
// provider credentials are only sent when the URL is same-origin with the
// configured base URL (never to a CDN or an attacker-named host).
func (m *ImageModel) downloadImage(ctx context.Context, imageURL string, callHeaders ...map[string]string) (*types.ImageResult, error) {
	baseURL := m.baseURL()
	var trustedTransport http.RoundTripper
	if c := m.provider.client.HTTPClient(); c != nil {
		trustedTransport = c.Transport
	}
	dlOpts := fileutil.TrustedOriginDownloadOptions(baseURL, trustedTransport)
	if fileutil.IsSameOrigin(imageURL, baseURL) {
		dlOpts.Headers = internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + providerutils.APIKeyOrEnv(m.provider.config.APIKey, "FIREWORKS_API_KEY"),
		}, m.provider.config.Headers)
		for _, h := range callHeaders {
			dlOpts.Headers = internalhttp.MergeHeaders(dlOpts.Headers, h)
		}
	}

	downloaded, err := fileutil.DownloadWithMetadata(ctx, imageURL, dlOpts)
	if err != nil {
		return nil, fmt.Errorf("Fireworks failed to download image: %w", err) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
	}
	data := downloaded.Data

	mimeType := downloaded.ContentType
	if mimeType == "" {
		mimeType = "image/png"
	}
	// Strip charset and other parameters (e.g. "image/jpeg; charset=utf-8" → "image/jpeg")
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	return &types.ImageResult{
		Image:    data,
		MimeType: mimeType,
	}, nil
}

// baseURL returns the developer-configured API base URL.
func (m *ImageModel) baseURL() string {
	if m.provider.config.BaseURL != "" {
		return m.provider.config.BaseURL
	}
	return "https://api.fireworks.ai/inference"
}

// buildWarnings mirrors the TS doGenerate warnings shared by the sync and
// async paths.
func (m *ImageModel) buildWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	warnings := []types.Warning{}
	supportsSize := fireworksImageGenerationModels[m.modelID]

	if !supportsSize && opts.Size != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "size",
			Details: "This model does not support the `size` option. Use `aspectRatio` instead.",
		})
	}
	// supportsSize doubles as "does not support aspectRatio" for the known set.
	if supportsSize && opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "This model does not support the `aspectRatio` option.",
		})
	}
	if len(opts.Files) > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "Fireworks only supports a single input image. Additional images are ignored.",
		})
	}
	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "mask",
			Details: "Fireworks Kontext models do not support explicit masks. Use the prompt to describe the areas to edit.",
		})
	}
	return warnings
}

// buildAsyncRequestBody builds the request body (TS doGenerate `body`), shared
// by the sync and async paths.
func (m *ImageModel) buildAsyncRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	if opts.N != nil {
		body["samples"] = *opts.N
	}

	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	if opts.Size != "" {
		parts := strings.SplitN(opts.Size, "x", 2)
		if len(parts) == 2 {
			body["width"] = parts[0]
			body["height"] = parts[1]
		}
	}

	// Handle input image for image editing
	if len(opts.Files) > 0 {
		if img := m.encodeImageFile(&opts.Files[0]); img != "" {
			body["input_image"] = img
		}
	}

	// Merge provider-specific options (passthrough); applied last like TS
	// `...fireworksOptions`.
	if opts.ProviderOptions != nil {
		if fwOpts, ok := opts.ProviderOptions["fireworks"].(map[string]interface{}); ok {
			for k, v := range fwOpts {
				body[k] = v
			}
		}
	}

	return body
}

// encodeImageFile encodes an image file to a data URI or URL string for the Fireworks API.
func (m *ImageModel) encodeImageFile(file *provider.ImageFile) string {
	if file.Type == "url" {
		return file.URL
	}
	if len(file.Data) > 0 {
		mediaType := file.MediaType
		if mediaType == "" {
			mediaType = "image/png"
		}
		return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(file.Data))
	}
	return ""
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	return m.buildAsyncRequestBody(opts)
}
