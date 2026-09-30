package fireworks

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	return "fireworks"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation, routing flux-kontext-* models through
// the async submission and polling path.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if m.isAsyncModel() {
		return m.doGenerateAsync(ctx, opts)
	}
	return m.doGenerateSync(ctx, opts)
}

// isAsyncModel returns true for flux-kontext-* models that require async polling.
func (m *ImageModel) isAsyncModel() bool {
	return strings.Contains(m.modelID, "flux-kontext")
}

// doGenerateSync handles synchronous image generation for standard models.
func (m *ImageModel) doGenerateSync(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	resp, err := m.provider.client.Post(ctx, "/v1/images/generations", reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("fireworks", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Fireworks AI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return m.convertResponse(resp.Body)
}

// doGenerateAsync handles async image generation for flux-kontext-* models.
// It submits a request, then polls until the image is ready.
func (m *ImageModel) doGenerateAsync(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	warnings := m.buildAsyncWarnings(opts)

	requestID, err := m.submitAsyncRequest(ctx, opts)
	if err != nil {
		return nil, err
	}

	result, err := m.pollAsyncResult(ctx, requestID)
	if err != nil {
		return nil, err
	}

	result.Warnings = warnings
	return result, nil
}

// submitAsyncRequest POSTs to the async submit endpoint and returns the request_id.
func (m *ImageModel) submitAsyncRequest(ctx context.Context, opts *provider.ImageGenerateOptions) (string, error) {
	body := m.buildAsyncRequestBody(opts)

	var submitResp AsyncSubmitResponse
	if err := m.provider.client.PostJSON(ctx, "/v1/workflows/"+m.modelID, body, &submitResp); err != nil {
		return "", fmt.Errorf("Fireworks async submit failed: %w", err)
	}

	if submitResp.RequestID == "" {
		return "", fmt.Errorf("Fireworks async submit returned empty request_id")
	}

	return submitResp.RequestID, nil
}

// checkAsyncStatus polls the status of an async generation job once.
// Returns the image URL when succeeded, whether the job is complete, and any error.
func (m *ImageModel) checkAsyncStatus(ctx context.Context, requestID string) (string, bool, error) {
	pollBody := map[string]interface{}{"id": requestID}

	var pollResp AsyncPollResponse
	if err := m.provider.client.PostJSON(ctx, "/v1/workflows/"+m.modelID+"/get_result", pollBody, &pollResp); err != nil {
		return "", false, fmt.Errorf("Fireworks async status check failed: %w", err)
	}

	switch pollResp.Status {
	case "Ready":
		if pollResp.Result == nil || pollResp.Result.Sample == nil {
			return "", false, fmt.Errorf("Fireworks poll response is Ready but missing result.sample")
		}
		return *pollResp.Result.Sample, true, nil
	case "Error", "Failed":
		return "", false, fmt.Errorf("Fireworks image generation failed with status: %s", pollResp.Status)
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
func (m *ImageModel) pollAsyncResult(ctx context.Context, requestID string) (*types.ImageResult, error) {
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
		imageURL, done, err := m.checkAsyncStatus(pollCtx, requestID)
		if err != nil {
			if timedOut.Load() {
				return nil, fmt.Errorf("Fireworks image generation timed out after %dms", timeoutMs)
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
			return m.downloadImage(ctx, imageURL)
		}

		select {
		case <-pollCtx.Done():
			if timedOut.Load() {
				return nil, fmt.Errorf("Fireworks image generation timed out after %dms", timeoutMs)
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
func (m *ImageModel) downloadImage(ctx context.Context, imageURL string) (*types.ImageResult, error) {
	baseURL := m.baseURL()
	var trustedTransport http.RoundTripper
	if c := m.provider.client.HTTPClient(); c != nil {
		trustedTransport = c.Transport
	}
	dlOpts := fileutil.TrustedOriginDownloadOptions(baseURL, trustedTransport)
	if fileutil.IsSameOrigin(imageURL, baseURL) {
		dlOpts.Headers = internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + m.provider.config.APIKey,
		}, m.provider.config.Headers)
	}

	downloaded, err := fileutil.DownloadWithMetadata(ctx, imageURL, dlOpts)
	if err != nil {
		return nil, fmt.Errorf("Fireworks failed to download image: %w", err)
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

// buildAsyncWarnings returns warnings for unsupported options on flux-kontext models,
// matching the TypeScript reference implementation.
func (m *ImageModel) buildAsyncWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	var warnings []types.Warning

	if opts.Size != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "size is not supported for flux-kontext models; use aspect_ratio instead",
		})
	}

	if len(opts.Files) > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "only the first input image is used; multiple files are not supported",
		})
	}

	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "mask is not supported for flux-kontext models",
		})
	}

	return warnings
}

// buildAsyncRequestBody builds the request body for async flux-kontext image generation.
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
		body["input_image"] = m.encodeImageFile(&opts.Files[0])
	}

	// Merge provider-specific options (passthrough)
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
	reqBody := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	if opts.N != nil {
		reqBody["n"] = *opts.N
	}

	if opts.Size != "" {
		var width, height int
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height) //nolint:errcheck
		if width > 0 && height > 0 {
			reqBody["width"] = width
			reqBody["height"] = height
		}
	}

	// Merge provider-specific options (passthrough). Matches TS, where the same
	// fireworks-image-model-options schema (cfg_scale/steps for legacy
	// image_generation models, plus guidance_scale/num_inference_steps/
	// output_format/webhook_url/webhook_secret/prompt_upsampling/
	// safety_tolerance) applies uniformly to both the sync and async paths.
	if opts.ProviderOptions != nil {
		if fwOpts, ok := opts.ProviderOptions["fireworks"].(map[string]interface{}); ok {
			for k, v := range fwOpts {
				reqBody[k] = v
			}
		}
	}

	return reqBody
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

	if response.Data[0].URL != "" {
		result.URL = response.Data[0].URL
		result.MimeType = "image/png"
	}

	if response.Data[0].B64JSON != "" {
		result.Image = []byte(response.Data[0].B64JSON)
		result.MimeType = "image/png"
	}

	return result, nil
}
