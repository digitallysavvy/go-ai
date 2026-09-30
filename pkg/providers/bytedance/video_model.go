package bytedance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// default polling configuration matching the AI SDK core's own default
// (5s interval / 10min timeout), used by DoGenerate's synthesized
// doStart+doStatus loop when the caller has not overridden the provider's
// (now deprecated) pollIntervalMs/pollTimeoutMs options.
const (
	defaultPollIntervalMs = 5000   // 5 seconds
	defaultPollTimeoutMs  = 600000 // 10 minutes
)

// VideoModel implements the provider.VideoModelV3 interface for ByteDance.
type VideoModel struct {
	prov    *Provider
	modelID string
}

// newVideoModel creates a new ByteDance video generation model
func newVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{
		prov:    prov,
		modelID: modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "bytedance.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns the maximum videos accepted per provider call.
func (m *VideoModel) MaxVideosPerCall() *int {
	maxVideos := 1
	return &maxVideos
}

func bytedanceGetFirstFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func bytedanceGetLastFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func bytedanceResolveStartImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	if img := bytedanceGetFirstFrameImage(opts); img != nil {
		return img
	}
	return opts.Image
}

func bytedanceIsVideoFile(f *provider.VideoModelV3File) bool {
	return f.MediaType != "" && strings.HasPrefix(f.MediaType, "video/")
}

// bytedanceResolveReferenceContent builds the reference image_url/video_url
// content entries, preferring inputReferences over the legacy
// referenceImages/referenceVideos provider options, and skipping all
// reference content when frameImages are provided (TS resolveReferenceContent).
func bytedanceResolveReferenceContent(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions, warnings *[]types.Warning) []map[string]interface{} {
	if len(opts.FrameImages) > 0 {
		return nil
	}

	if len(opts.InputReferences) > 0 {
		content := make([]map[string]interface{}, 0, len(opts.InputReferences))
		for i := range opts.InputReferences {
			reference := &opts.InputReferences[i]
			if reference.Type == "url" && reference.MediaType == "" {
				*warnings = append(*warnings, types.Warning{
					Type:    "unsupported",
					Feature: "inputReferences",
					Details: "ByteDance requires an explicit mediaType to route URL references as video or image. Pass { data: url, mediaType: \"video/mp4\" } for video references. The reference was treated as an image.",
				})
			}

			url, err := encodeImage(reference)
			if err != nil {
				continue
			}
			if bytedanceIsVideoFile(reference) {
				content = append(content, map[string]interface{}{
					"type":      "video_url",
					"video_url": map[string]interface{}{"url": url},
					"role":      "reference_video",
				})
			} else {
				content = append(content, map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": url},
					"role":      "reference_image",
				})
			}
		}
		return content
	}

	var content []map[string]interface{}
	for _, imageURL := range provOpts.ReferenceImages {
		content = append(content, map[string]interface{}{
			"type":      "image_url",
			"image_url": map[string]interface{}{"url": imageURL},
			"role":      "reference_image",
		})
	}
	for _, videoURL := range provOpts.ReferenceVideos {
		content = append(content, map[string]interface{}{
			"type":      "video_url",
			"video_url": map[string]interface{}{"url": videoURL},
			"role":      "reference_video",
		})
	}
	return content
}

// bytedanceResolveLastFrameImage resolves the last_frame image URL/data URI,
// preferring a frameImages last_frame over the (legacy) lastFrameImage
// provider option.
func bytedanceResolveLastFrameImage(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) *string {
	if lastFrame := bytedanceGetLastFrameImage(opts); lastFrame != nil {
		encoded, err := encodeImage(lastFrame)
		if err == nil {
			return &encoded
		}
	}
	return provOpts.LastFrameImage
}

// DoStart starts an asynchronous video generation via ByteDance's task
// creation endpoint and returns an opaque operation reference
// (TS ByteDanceVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	provOpts, err := extractProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	warnings := []types.Warning{}

	// Polling is orchestrated by the AI SDK core via doStart/doStatus, so the
	// legacy provider-level poll options no longer have any effect on real
	// (poll/webhook-driven) calls; DoGenerate still honors them internally as
	// its own default poll behavior (see the doc comment on DoGenerate).
	if provOpts.PollIntervalMs != nil {
		warnings = append(warnings, types.Warning{
			Type:    "deprecated",
			Setting: "pollIntervalMs",
			Message: "`pollIntervalMs` is ignored. Polling is orchestrated by the AI SDK: pass `poll: { intervalMs, timeoutMs }` to `generateVideo` instead.",
		})
	}
	if provOpts.PollTimeoutMs != nil {
		warnings = append(warnings, types.Warning{
			Type:    "deprecated",
			Setting: "pollTimeoutMs",
			Message: "`pollTimeoutMs` is ignored. Polling is orchestrated by the AI SDK: pass `poll: { intervalMs, timeoutMs }` to `generateVideo` instead.",
		})
	}

	if opts.FPS != nil && *opts.FPS != 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "fps",
			Details: "ByteDance video models do not support custom FPS. Frame rate is fixed at 24 fps.",
		})
	}

	if opts.N > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "n",
			Details: "ByteDance video models do not support generating multiple videos per call. Only 1 video will be generated.",
		})
	}

	body, err := m.buildRequestBody(&opts.VideoModelV3CallOptions, provOpts)
	if err != nil {
		return nil, err
	}

	// Progress notifications require a protocol-aware receiver, so we don't
	// implement VideoModelWebhookHandler; forward webhookUrl as callback_url
	// for a caller-owned receiver that filters progress notifications.
	if opts.WebhookURL != "" {
		body["callback_url"] = opts.WebhookURL
	}

	submitResp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/contents/generations/tasks",
		Headers: opts.Headers,
		Body:    body,
	})
	if err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to submit request: %v", err))
	}
	if submitResp.StatusCode >= 400 {
		return nil, m.parseAPIError(submitResp.Body, submitResp.StatusCode)
	}

	var createResp taskCreateResponse
	if err := json.Unmarshal(submitResp.Body, &createResp); err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to parse creation response: %v", err))
	}
	if createResp.ID == "" {
		return nil, NewVideoGenerationError("No task ID returned from API")
	}

	operation, _ := json.Marshal(bytedanceOperation{TaskID: createResp.ID})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   flattenHeaders(submitResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via ByteDance's task status endpoint
// (TS ByteDanceVideoModel#doStatus). The status URL is fetched through
// fileutil's validated-redirect poller, matching TS's
// `getFromApi({ validateUrl: true, trustedOrigin })`: any redirect away from
// the provider's own base URL is SSRF-validated and dialed through a
// DNS-pinning transport, and credentials are only forwarded on the trusted
// hop.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	var operation bytedanceOperation
	if err := json.Unmarshal(opts.Operation, &operation); err != nil {
		return nil, fmt.Errorf("bytedance: invalid operation reference: %w", err)
	}

	downloadOpts := fileutil.TrustedOriginDownloadOptions(m.prov.config.BaseURL, nil)
	downloadOpts.Headers = internalhttp.MergeHeaders(m.prov.reqHeaders, opts.Headers)

	pollURL := strings.TrimRight(m.prov.config.BaseURL, "/") + "/contents/generations/tasks/" + operation.TaskID

	var statusResp taskStatusResponse
	result, err := fileutil.PollJSON(ctx, pollURL, downloadOpts, &statusResp)
	if err != nil {
		return nil, m.handlePollError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: time.Now(),
		ModelID:   m.modelID,
		Headers:   flattenHeaderSlice(result.Headers),
	}

	switch statusResp.Status {
	case "succeeded":
		videoURL := ""
		lastFrameURL := ""
		if statusResp.Content != nil {
			videoURL = statusResp.Content.VideoURL
			lastFrameURL = statusResp.Content.LastFrameURL
		}
		if videoURL == "" {
			return nil, NewVideoGenerationError(fmt.Sprintf("No video URL in response. Task ID: %s", operation.TaskID))
		}

		// Assign through an interface{} local rather than the *taskUsageField
		// directly: a nil *taskUsageField boxed straight into the map would
		// compare non-nil (a typed nil), unlike TS's plain `undefined`.
		var usage interface{}
		if statusResp.Usage != nil {
			usage = statusResp.Usage
		}
		meta := map[string]interface{}{
			"taskId": operation.TaskID,
			"usage":  usage,
		}
		if lastFrameURL != "" {
			meta["lastFrameUrl"] = lastFrameURL
		}

		return &provider.VideoModelV3OperationStatusResult{
			Status: provider.VideoOperationStatusCompleted,
			Videos: []provider.VideoModelV3VideoData{
				{Type: "url", URL: videoURL, MediaType: "video/mp4"},
			},
			ProviderMetadata: map[string]interface{}{"bytedance": meta},
			Response:         responseInfo,
		}, nil

	// ModelArk documents "cancelled"; "canceled" is handled defensively.
	case "failed", "expired", "cancelled", "canceled":
		details := ""
		if statusResp.Error != nil {
			if statusResp.Error.Message != "" {
				details = statusResp.Error.Message
			} else if statusResp.Error.Code != "" {
				details = statusResp.Error.Code
			}
		}
		if details == "" {
			details = mustMarshal(statusResp)
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    fmt.Sprintf("Video generation %s. Task ID: %s. %s", statusResp.Status, operation.TaskID, details),
			Response: responseInfo,
		}, nil

	default:
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusPending,
			Response: responseInfo,
		}, nil
	}
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes. Go's VideoModelV3 always requires
// DoGenerate (unlike TS, where this model implements only
// doStart/doStatus and the core generate-video flow polls it directly), so
// this method is the Go equivalent of that default polling behavior, built
// entirely on top of DoStart/DoStatus rather than a separate request
// implementation.
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	provOpts, _ := extractProviderOptions(opts.ProviderOptions)
	pollIntervalMs := defaultPollIntervalMs
	pollTimeoutMs := defaultPollTimeoutMs
	if provOpts.PollIntervalMs != nil {
		pollIntervalMs = *provOpts.PollIntervalMs
	}
	if provOpts.PollTimeoutMs != nil {
		pollTimeoutMs = *provOpts.PollTimeoutMs
	}

	var finalStatus *provider.VideoModelV3OperationStatusResult
	var jobFailureErr error

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.Headers,
		})
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case provider.VideoOperationStatusCompleted:
			finalStatus = status
			return &polling.JobResult{Status: polling.JobStatusCompleted}, nil
		case provider.VideoOperationStatusError:
			jobFailureErr = errors.New(status.Error)
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: status.Error}, nil
		default:
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		}
	}

	pollOpts := polling.PollOptions{PollIntervalMs: pollIntervalMs, PollTimeoutMs: pollTimeoutMs}
	_, pollErr := polling.PollForCompletion(ctx, checker, pollOpts)
	if pollErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("video generation aborted: %w", ctx.Err())
		}
		if jobFailureErr != nil {
			return nil, jobFailureErr
		}
		// A genuine DoStatus error (e.g. "No video URL in response") comes
		// back from the checker and is wrapped by PollForCompletion as
		// "status check failed: ...": propagate it as-is rather than
		// reporting a misleading timeout. Only an actual elapsed-timeout
		// (or max-attempts) error from PollForCompletion becomes
		// NewTimeoutError.
		if strings.Contains(pollErr.Error(), "polling timeout") || strings.Contains(pollErr.Error(), "max polling attempts") {
			return nil, NewTimeoutError(fmt.Sprintf("%dms", pollTimeoutMs))
		}
		return nil, pollErr
	}

	return &provider.VideoModelV3Response{
		Videos:           finalStatus.Videos,
		Warnings:         append(append([]types.Warning{}, startResult.Warnings...), finalStatus.Warnings...),
		ProviderMetadata: finalStatus.ProviderMetadata,
		Response:         finalStatus.Response,
	}, nil
}

// buildRequestBody builds the API request body from call options
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, error) {
	var warnings []types.Warning // unused feature warnings are attached by DoStart; kept local for resolveReferenceContent

	content := []map[string]interface{}{}

	// Go cannot distinguish an omitted optional string from an explicit empty
	// string. Map the zero value to TS's omitted prompt behavior.
	if opts.Prompt != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": opts.Prompt,
		})
	}

	startImage := bytedanceResolveStartImage(opts)
	lastFrameImageURL := bytedanceResolveLastFrameImage(opts, provOpts)
	referenceContent := bytedanceResolveReferenceContent(opts, provOpts, &warnings)

	if startImage != nil {
		imageURL, err := encodeImage(startImage)
		if err != nil {
			return nil, fmt.Errorf("failed to encode image: %w", err)
		}
		imagePart := map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": imageURL,
			},
		}
		if lastFrameImageURL != nil {
			imagePart["role"] = "first_frame"
		} else if len(referenceContent) > 0 {
			imagePart["role"] = "reference_image"
		}
		content = append(content, imagePart)
	}

	// Add last frame image if provided
	if lastFrameImageURL != nil {
		content = append(content, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": *lastFrameImageURL,
			},
			"role": "last_frame",
		})
	}

	content = append(content, referenceContent...)

	// Add reference audio if provided
	for _, refURL := range provOpts.ReferenceAudio {
		content = append(content, map[string]interface{}{
			"type": "audio_url",
			"audio_url": map[string]interface{}{
				"url": refURL,
			},
			"role": "reference_audio",
		})
	}

	body := map[string]interface{}{
		"model":   m.modelID,
		"content": content,
	}

	// Standard options
	if opts.AspectRatio != "" {
		body["ratio"] = opts.AspectRatio
	}

	if opts.Duration != nil && *opts.Duration != 0 {
		body["duration"] = *opts.Duration
	}

	if opts.Seed != nil && *opts.Seed != 0 {
		body["seed"] = *opts.Seed
	}

	if opts.Resolution != "" {
		body["resolution"] = mapResolution(opts.Resolution)
	}

	// Provider-specific options
	generateAudio := opts.GenerateAudio
	if generateAudio == nil {
		generateAudio = provOpts.GenerateAudio
	}
	if generateAudio != nil {
		body["generate_audio"] = *generateAudio
	}

	if provOpts.Watermark != nil {
		body["watermark"] = *provOpts.Watermark
	}

	if provOpts.CameraFixed != nil {
		body["camera_fixed"] = *provOpts.CameraFixed
	}

	if provOpts.ReturnLastFrame != nil {
		body["return_last_frame"] = *provOpts.ReturnLastFrame
	}

	if provOpts.ServiceTier != nil {
		body["service_tier"] = *provOpts.ServiceTier
	}

	if provOpts.Draft != nil {
		body["draft"] = *provOpts.Draft
	}

	// Passthrough additional options
	for k, v := range provOpts.Additional {
		body[k] = v
	}

	return body, nil
}

// parseAPIError parses a ByteDance API error response
func (m *VideoModel) parseAPIError(body []byte, statusCode int) error {
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		msg := ""
		if errResp.Error != nil {
			msg = errResp.Error.Message
		} else if errResp.Message != "" {
			msg = errResp.Message
		}
		if msg != "" {
			return NewError(statusCode, msg, "")
		}
	}
	return NewError(statusCode, fmt.Sprintf("API returned status %d", statusCode), string(body))
}

// handlePollError converts a fileutil poll error (a providererrors.DownloadError
// for a non-2xx HTTP response, or a validation/network error) into a
// ByteDance API error where possible.
func (m *VideoModel) handlePollError(err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		return m.parseAPIError(dlErr.Body, dlErr.StatusCode)
	}
	return NewVideoGenerationError(fmt.Sprintf("failed to check status: %v", err))
}

// encodeImage encodes a VideoModelV3File to a URL or data URI
func encodeImage(img *provider.VideoModelV3File) (string, error) {
	if img.Type == "url" {
		return img.URL, nil
	}
	if img.Type == "file" {
		encoded := base64.StdEncoding.EncodeToString(img.Data)
		return fmt.Sprintf("data:%s;base64,%s", img.MediaType, encoded), nil
	}
	return "", fmt.Errorf("unsupported image type: %s", img.Type)
}

// extractProviderOptions extracts ByteDance provider options from the generic options map
func extractProviderOptions(opts map[string]interface{}) (*ProviderOptions, error) {
	if opts == nil {
		return &ProviderOptions{}, nil
	}

	bdOpts, ok := opts["bytedance"]
	if !ok {
		return &ProviderOptions{}, nil
	}

	// Convert to JSON and back to get proper typed struct
	jsonData, err := json.Marshal(bdOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bytedance provider options: %w", err)
	}

	var provOpts ProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bytedance provider options: %w", err)
	}
	if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs <= 0 {
		return nil, fmt.Errorf("invalid bytedance provider option pollIntervalMs: must be positive")
	}
	if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs <= 0 {
		return nil, fmt.Errorf("invalid bytedance provider option pollTimeoutMs: must be positive")
	}

	// Collect additional/passthrough options not in the struct
	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawMap); err != nil {
		return &provOpts, nil
	}

	handled := map[string]bool{
		"watermark":       true,
		"generateAudio":   true,
		"cameraFixed":     true,
		"returnLastFrame": true,
		"serviceTier":     true,
		"draft":           true,
		"lastFrameImage":  true,
		"referenceImages": true,
		"referenceVideos": true,
		"referenceAudio":  true,
		"pollIntervalMs":  true,
		"pollTimeoutMs":   true,
	}

	additional := map[string]interface{}{}
	for k, v := range rawMap {
		if !handled[k] {
			additional[k] = v
		}
	}
	if len(additional) > 0 {
		provOpts.Additional = additional
	}

	return &provOpts, nil
}

// mustMarshal marshals a value to JSON string, returning "{}" on error
func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func flattenHeaders(headers map[string][]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func flattenHeaderSlice(headers map[string][]string) map[string]string {
	return flattenHeaders(headers)
}

// bytedanceOperation is the opaque operation reference returned by DoStart
// and passed back into DoStatus.
type bytedanceOperation struct {
	TaskID string `json:"taskId"`
}

// API response types

// taskCreateResponse is the response from task creation
type taskCreateResponse struct {
	ID string `json:"id"`
}

// taskStatusResponse is the response from task status polling
type taskStatusResponse struct {
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Status  string            `json:"status"`
	Content *taskContentField `json:"content"`
	Usage   *taskUsageField   `json:"usage"`
	// Error is present on failed tasks (the HTTP response itself is still 200).
	Error *errorDetail `json:"error"`
}

// taskContentField holds the video URL in the status response
type taskContentField struct {
	VideoURL     string `json:"video_url"`
	LastFrameURL string `json:"last_frame_url"`
}

// taskUsageField holds token usage info
type taskUsageField struct {
	CompletionTokens int `json:"completion_tokens"`
}

// errorResponse is the error response format from ByteDance
type errorResponse struct {
	Error   *errorDetail `json:"error"`
	Message string       `json:"message"`
}

// errorDetail holds the error message and code
type errorDetail struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}
