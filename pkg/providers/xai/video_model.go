package xai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// VideoModel implements the provider.VideoModelV3 interface for XAI
type VideoModel struct {
	provider *Provider
	modelID  string
}

type VideoExtendOptions struct {
	Video    string
	Prompt   string
	Duration *float64
	Headers  map[string]string
}

// NewVideoModel creates a new XAI video generation model
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{
		provider: prov,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "xai.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (XAI generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	maxVideos := 1
	return &maxVideos
}

// Extend extends an existing video using xAI extension mode.
func (m *VideoModel) Extend(ctx context.Context, opts VideoExtendOptions) (*provider.VideoModelV3Response, error) {
	providerOptions := map[string]interface{}{
		"xai": map[string]interface{}{
			"mode":     "extend-video",
			"videoUrl": opts.Video,
		},
	}
	return m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:          opts.Prompt,
		Duration:        opts.Duration,
		ProviderOptions: providerOptions,
		Headers:         opts.Headers,
	})
}

// XAIVideoProviderOptions contains provider-specific options for XAI video generation
type XAIVideoProviderOptions struct {
	// PollIntervalMs is the interval between status checks in milliseconds (default: 5000)
	PollIntervalMs *int `json:"pollIntervalMs,omitempty"`

	// PollTimeoutMs is the maximum time to wait for video generation in milliseconds (default: 600000)
	PollTimeoutMs *int `json:"pollTimeoutMs,omitempty"`

	// Resolution is the output resolution: "480p", "720p", or "1080p"
	Resolution *string `json:"resolution,omitempty"`

	// VideoURL is the source video URL for video editing/extension
	VideoURL *string `json:"videoUrl,omitempty"`

	// Mode selects the operation: edit-video, extend-video, reference-to-video
	Mode *string `json:"mode,omitempty"`

	// ReferenceImageURLs are reference image URLs (1-7) for reference-to-video mode
	ReferenceImageURLs []string `json:"referenceImageUrls,omitempty"`

	// ReferenceVoiceIDs are preset voice ids (up to 3) that give the R2V
	// subject a voice. Only applies to reference-to-video generation.
	ReferenceVoiceIDs []string `json:"referenceVoiceIds,omitempty"`

	// Keyframes are mid-video image anchors (up to 4). Only supported by
	// "grok-imagine-video-1.5" for standard generation (not edit/extension).
	Keyframes []XAIVideoKeyframe `json:"keyframes,omitempty"`

	// StorageOptions persists the generated video in the xAI Files API.
	StorageOptions *XAIVideoStorageOptions `json:"storageOptions,omitempty"`

	// User is a unique identifier representing the end user, for abuse
	// monitoring. Not sent for video extension requests.
	User *string `json:"user,omitempty"`

	// Output delivers the video directly to caller-managed storage through
	// an HTTPS signed PUT URL (TS 2e44144812). The completed result then
	// reports an uploaded video (provider.VideoDataTypeUploaded) with no
	// URL, and the signed URL never appears in errors, warnings, provider
	// metadata or the operation reference. The raw form
	// {"output": {"upload_url": ...}} that v0.5.2 passed through is still
	// accepted and validated the same way; uploadUrl wins.
	Output *XAIVideoOutput `json:"output,omitempty"`
}

// XAIVideoOutput is the upload delivery destination.
type XAIVideoOutput struct {
	// UploadURL is an https URL without userinfo or fragment.
	UploadURL string `json:"uploadUrl"`
}

// XAIVideoKeyframe is a mid-video image anchor.
type XAIVideoKeyframe struct {
	ImageURL         string  `json:"imageUrl"`
	TimestampSeconds float64 `json:"timestampSeconds"`
}

// XAIVideoStorageOptions requests that the generated video be persisted in
// the xAI Files API.
type XAIVideoStorageOptions struct {
	Filename     string `json:"filename"`
	ExpiresAfter *int   `json:"expiresAfter,omitempty"`

	// PublicURL is either a bool or an object with an ExpiresAfter field
	// (XAIVideoPublicURLOptions). Both shapes are supported by xAI; use
	// PublicURLBool / PublicURLWithExpiry to construct one.
	PublicURL interface{} `json:"publicUrl,omitempty"`
}

// XAIVideoPublicURLOptions is the object form of
// XAIVideoStorageOptions.PublicURL.
type XAIVideoPublicURLOptions struct {
	ExpiresAfter *int `json:"expiresAfter,omitempty"`
}

// xaiVideoOperation is the opaque operation reference returned by DoStart
// and passed back into DoStatus.
type xaiVideoOperation struct {
	RequestID string `json:"requestId"`
	// HasUploadURL marks an upload delivery (TS operation.hasUploadUrl); the
	// signed URL itself is never stored.
	HasUploadURL bool `json:"hasUploadUrl,omitempty"`
}

// xaiUploadError rebuilds an API error of an upload delivery call without
// the request, response body or cause, keeping the status and retryability
// (TS 2e44144812: the error could echo the signed URL).
func xaiUploadError(err error, message string) error {
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		return err
	}
	retryable := pe.IsRetryable()
	return &providererrors.ProviderError{Provider: "xai", StatusCode: pe.StatusCode, Message: message, Retryable: &retryable}
}

// DoStart starts an asynchronous video generation/edit/extension via the
// mode-appropriate xAI endpoint and returns an opaque operation reference
// (TS XaiVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	currentDate := time.Now()
	callOpts := &opts.VideoModelV3CallOptions
	warnings := []types.Warning{}

	provOpts, extra, err := extractVideoProviderOptions(callOpts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	mode := resolveMode(callOpts, provOpts)
	isEdit := mode == "edit-video"
	isExtension := mode == "extend-video"
	hasReferenceImages := mode == "reference-to-video"

	// Check for unsupported options and add warnings
	warnings = append(warnings, m.checkUnsupportedOptions(callOpts, provOpts, mode)...)

	// Build request body
	body, bodyWarnings := m.buildRequestBody(callOpts, provOpts, extra, isEdit, isExtension, hasReferenceImages)
	warnings = append(warnings, bodyWarnings...)

	// Determine endpoint
	endpoint := "/videos/generations"
	if isEdit {
		endpoint = "/videos/edits"
	} else if isExtension {
		endpoint = "/videos/extensions"
	}

	// Submit video generation/edit/extension request
	var createResp xaiVideoCreateResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator("xai", providerutils.SchemaXAIVideoCreate),
		Method:           http.MethodPost,
		Path:             endpoint,
		Body:             body,
		Headers:          callOpts.Headers,
	}, &createResp)
	if err != nil {
		if provOpts.Output != nil {
			return nil, xaiUploadError(m.handleError(err), "xAI video upload request failed.")
		}
		return nil, m.handleError(err)
	}

	if createResp.RequestID == "" {
		return nil, providererrors.NewProviderError("xai", 0, "",
			"No request_id returned from xAI API.", nil)
	}

	operation, _ := json.Marshal(xaiVideoOperation{RequestID: createResp.RequestID, HasUploadURL: provOpts.Output != nil})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   convertXAIHeaders(httpResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation/edit/
// extension started with DoStart via xAI's video status endpoint (TS
// XaiVideoModel#doStatus).
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	currentDate := time.Now()

	var op xaiVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("xai: invalid operation reference: %w", err)
	}

	status, headers, err := m.checkVideoStatus(ctx, op.RequestID, opts.Headers)
	if err != nil {
		if op.HasUploadURL {
			return nil, xaiUploadError(m.handleError(err), "xAI video upload status request failed.")
		}
		return nil, m.handleError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   headers,
	}

	if status.is("expired") {
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    "Video generation request expired.",
			Response: responseInfo,
		}, nil
	}

	if status.is("failed") {
		// TS statusResponse.error?.message ?? statusResponse.error?.code.
		var errDetails *string
		if status.Error != nil {
			errDetails = status.Error.Message
			if errDetails == nil {
				errDetails = status.Error.Code
			}
		}
		msg := "Video generation failed."
		if errDetails != nil && !op.HasUploadURL {
			msg = fmt.Sprintf("Video generation failed: %s", *errDetails)
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    msg,
			Response: responseInfo,
		}, nil
	}

	// TS completes a status-less response only on video.url (not file_output.public_url).
	if status.is("done") || (status.Status == nil && status.Video != nil && status.Video.URL != nil && *status.Video.URL != "") {
		// Terminal outcomes (moderation rejection, missing URL) are reported
		// as an error status rather than a thrown Go error, so they surface
		// the same way as any other job failure.
		if status.Video != nil && status.Video.RespectModeration != nil && !*status.Video.RespectModeration {
			return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusError,
				Error:    "Video generation was blocked due to a content policy violation.",
				Response: responseInfo,
			}, nil
		}

		videoURL := resolveVideoURL(status.Video)
		if videoURL == "" && !op.HasUploadURL {
			return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusError,
				Error:    "Video generation completed but no video URL was returned.",
				Response: responseInfo,
			}, nil
		}

		// Status-level warnings (beyond the TS response schema, kept for
		// backward compatibility with existing callers of this Go SDK).
		var statusWarnings []types.Warning
		for _, w := range status.Warnings {
			msg := w.Message
			if msg == "" {
				msg = w.Code
			}
			if msg != "" {
				statusWarnings = append(statusWarnings, types.Warning{
					Type:    "provider-warning",
					Details: msg,
					Message: msg,
				})
			}
		}

		videos := []provider.VideoModelV3VideoData{{Type: provider.VideoDataTypeURL, URL: videoURL, MediaType: "video/mp4"}}
		meta := buildXAIVideoMetadata(op.RequestID, status)
		if op.HasUploadURL {
			// TS 2e44144812: an upload delivery reports the uploaded outcome
			// and never exposes a video URL.
			videos = []provider.VideoModelV3VideoData{{Type: provider.VideoDataTypeUploaded, MediaType: "video/mp4"}}
			delete(meta, "videoUrl")
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:           provider.VideoOperationStatusCompleted,
			Videos:           videos,
			Warnings:         statusWarnings,
			ProviderMetadata: map[string]interface{}{"xai": meta},
			Response:         responseInfo,
		}, nil
	}

	return &provider.VideoModelV3OperationStatusResult{
		Status:   provider.VideoOperationStatusPending,
		Response: responseInfo,
	}, nil
}

// buildXAIVideoMetadata builds the xai-scoped providerMetadata object from a
// completed status response (TS doStatus's inline providerMetadata.xai
// construction).
func buildXAIVideoMetadata(requestID string, status *xaiVideoStatusResponse) map[string]interface{} {
	videoData := status.Video
	meta := map[string]interface{}{
		"requestId": requestID,
		"videoUrl":  resolveVideoURL(videoData),
	}
	if videoData != nil && videoData.Duration != nil {
		meta["duration"] = *videoData.Duration
	}
	// Cost is in the top-level usage object, not inside the video object.
	if status.Usage != nil && status.Usage.CostInUsdTicks != nil {
		meta["costInUsdTicks"] = *status.Usage.CostInUsdTicks
	}
	if status.Progress != nil {
		// Whole-number progress stays an int (the pre-existing metadata type).
		if p := *status.Progress; p == math.Trunc(p) {
			meta["progress"] = int(p)
		} else {
			meta["progress"] = p
		}
	}
	if videoData != nil && videoData.FileOutput != nil {
		fileOutput := map[string]interface{}{
			"fileId":   videoData.FileOutput.FileID,
			"filename": videoData.FileOutput.Filename,
		}
		if videoData.FileOutput.ExpiresAt != nil {
			fileOutput["expiresAt"] = *videoData.FileOutput.ExpiresAt
		}
		if videoData.FileOutput.PublicURL != nil {
			fileOutput["publicUrl"] = *videoData.FileOutput.PublicURL
		}
		if videoData.FileOutput.PublicURLError != nil {
			fileOutput["publicUrlError"] = *videoData.FileOutput.PublicURLError
		}
		if videoData.FileOutput.PublicURLExpiresAt != nil {
			fileOutput["publicUrlExpiresAt"] = *videoData.FileOutput.PublicURLExpiresAt
		}
		meta["fileOutput"] = fileOutput
	}
	if videoData != nil && videoData.StorageError != nil {
		meta["storageError"] = *videoData.StorageError
	}
	return meta
}

// convertXAIHeaders is the response headers of a video call: TS
// extractResponseHeaders joins repeated lines with ", " (the last Set-Cookie
// wins). Keys keep the Go canonical form of v0.5.2 (TS: lower case; S6).
func convertXAIHeaders(h http.Header) map[string]string {
	if out := providerutils.ExtractHeaders(h); out != nil {
		return out
	}
	return map[string]string{}
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes. Go's VideoModelV3 always requires
// DoGenerate (unlike TS, where this model implements only
// doStart/doStatus and the core generate-video flow polls it directly), so
// this method is the Go equivalent of that default polling behavior, built
// entirely on top of DoStart/DoStatus.
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	provOpts, _, _ := extractVideoProviderOptions(opts.ProviderOptions)
	pollOpts := m.getPollOptions(provOpts)

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
			jobFailureErr = providererrors.NewProviderError("xai", 0, "", status.Error, nil)
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: status.Error}, nil
		default:
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		}
	}

	_, pollErr := polling.PollForCompletion(ctx, checker, pollOpts)
	if pollErr != nil {
		if jobFailureErr != nil {
			return nil, jobFailureErr
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

// VideoStartStatusPollDefaults marks the model as start/status only (TS
// XaiVideoModel has no doGenerate), so ai.GenerateVideo drives it through the
// core start/status flow. It returns the v0.5.2 pollIntervalMs /
// pollTimeoutMs provider options (0 when unset: the core 5000 / 600000
// defaults), which TS ignores; Go keeps honoring them (v0.5.2 contract, S6).
func (m *VideoModel) VideoStartStatusPollDefaults(providerOptions map[string]interface{}) (intervalMs, timeoutMs int) {
	provOpts, _, err := extractVideoProviderOptions(providerOptions)
	if err != nil || provOpts == nil {
		return 0, 0
	}
	if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs > 0 {
		intervalMs = *provOpts.PollIntervalMs
	}
	if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs > 0 {
		timeoutMs = *provOpts.PollTimeoutMs
	}
	return intervalMs, timeoutMs
}

// getPollOptions extracts polling options from XAIVideoProviderOptions,
// applying the same 5s/600s defaults DoGenerate previously hardcoded.
func (m *VideoModel) getPollOptions(provOpts *XAIVideoProviderOptions) polling.PollOptions {
	pollOpts := polling.PollOptions{PollIntervalMs: 5000, PollTimeoutMs: 600000}
	if provOpts != nil {
		if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs > 0 {
			pollOpts.PollIntervalMs = *provOpts.PollIntervalMs
		}
		if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs > 0 {
			pollOpts.PollTimeoutMs = *provOpts.PollTimeoutMs
		}
	}
	return pollOpts
}

// maxPendingStatusBodyBytes bounds how much of a 202 (still processing)
// status response body we attempt to parse. xAI answers 202 while a
// generation is still running, sometimes with an empty body; this is a
// generous bound for a `{status, progress}` payload of roughly 50 bytes.
const maxPendingStatusBodyBytes = 1024 * 1024

// checkVideoStatus fetches the current status of a video generation/edit/
// extension job. It uses the lower-level client.Do (rather than DoJSON) so
// a 202 response with an empty or non-JSON body can be treated as "pending"
// instead of failing on JSON unmarshal. Returns the flattened response
// headers alongside the parsed status, matching TS doStatus's
// responseHeaders.
func (m *VideoModel) checkVideoStatus(ctx context.Context, requestID string, headers map[string]string) (*xaiVideoStatusResponse, map[string]string, error) {
	statusPath := "/videos/" + providerutils.EncodePathSegment(requestID)

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    statusPath,
		Headers: headers,
	})
	if err != nil {
		return nil, nil, err
	}

	respHeaders := convertXAIHeaders(resp.Headers)

	if resp.StatusCode == 202 {
		// Bound how much of the body we attempt to parse. An empty or
		// non-JSON payload is treated as a pending status, but an oversized
		// one is a real error (row 2b872b0 / item 8) -- mirroring TS
		// readPendingBody, which throws an APICallError once the streamed
		// body exceeds MAX_PENDING_BODY_BYTES rather than silently guessing
		// "pending" for a payload that large.
		limited, readErr := io.ReadAll(io.LimitReader(bytes.NewReader(resp.Body), maxPendingStatusBodyBytes+1))
		if readErr != nil {
			return &xaiVideoStatusResponse{Status: &xaiVideoPending}, respHeaders, nil
		}
		if len(limited) > maxPendingStatusBodyBytes {
			return nil, nil, providererrors.NewProviderError("xai", resp.StatusCode, "",
				fmt.Sprintf("xAI video status response exceeded %d bytes", maxPendingStatusBodyBytes), nil)
		}
		// TS readPendingBody decodes with TextDecoder (leading BOM dropped,
		// one U+FFFD per invalid subpart) and treats text.trim() == "" as
		// pending.
		text := prompt.DecodeUTF8Text(limited)
		if providerutils.JSTrim(text) == "" {
			return &xaiVideoStatusResponse{Status: &xaiVideoPending}, respHeaders, nil
		}

		var status xaiVideoStatusResponse
		if err := providerutils.UnmarshalChunk([]byte(text), &status); err != nil {
			return &xaiVideoStatusResponse{Status: &xaiVideoPending}, respHeaders, nil
		}
		return &status, respHeaders, nil
	}

	if resp.StatusCode >= 400 {
		return nil, nil, newXAIProviderError("xai", resp.StatusCode, resp.Headers, resp.Body)
	}

	// TS xaiVideoStatusJsonResponseHandler: createJsonResponseHandler(schema),
	// which parses response.text() (UTF-8 decoded, leading BOM dropped).
	body := []byte(prompt.DecodeUTF8Text(resp.Body))
	if err := providerutils.MediaResponseValidator("xai", providerutils.SchemaXAIVideoStatus)(resp.StatusCode, resp.Headers, body); err != nil {
		return nil, nil, err
	}
	var status xaiVideoStatusResponse
	if err := providerutils.UnmarshalChunk(body, &status); err != nil {
		return nil, nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return &status, respHeaders, nil
}

// buildRequestBody constructs the API request body
func (m *VideoModel) buildRequestBody(
	opts *provider.VideoModelV3CallOptions,
	provOpts *XAIVideoProviderOptions,
	extra map[string]interface{},
	isEdit bool,
	isExtension bool,
	hasReferenceImages bool,
) (map[string]interface{}, []types.Warning) {
	warnings := []types.Warning{}

	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	// Add duration (not for edits; extension allows duration)
	if !isEdit && opts.Duration != nil {
		body["duration"] = *opts.Duration
	}

	// Add aspect ratio (not for edits or extension)
	if !isEdit && !isExtension && opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	// Add resolution (not for edits or extension)
	if !isEdit && !isExtension && provOpts.Resolution != nil {
		body["resolution"] = *provOpts.Resolution
	} else if !isEdit && !isExtension && opts.Resolution != "" {
		// Map standard resolution to XAI format
		mapped := mapResolution(opts.Resolution)
		if mapped != "" {
			body["resolution"] = mapped
		}
	}

	// GenerateAudio requests generated audio alongside the video. Not
	// supported for edit/extension.
	if opts.GenerateAudio != nil && !isEdit && !isExtension {
		body["generate_audio"] = *opts.GenerateAudio
	} else if opts.GenerateAudio != nil {
		mode := "video editing"
		if isExtension {
			mode = "video extension"
		}
		warnings = append(warnings, unsupportedVideoWarning("generateAudio",
			fmt.Sprintf("xAI %s does not support generateAudio.", mode)))
	}

	// TS 2e44144812: upload delivery.
	if provOpts.Output != nil {
		body["output"] = map[string]interface{}{"upload_url": provOpts.Output.UploadURL}
	}

	// Persist the generated video in the xAI Files API.
	if provOpts.StorageOptions != nil {
		body["storage_options"] = storageOptionsToWire(provOpts.StorageOptions)
	}

	// Mid-video image anchors. Only grok-imagine-video-1.5 supports them,
	// and only for standard generation (not edit/extension).
	if len(provOpts.Keyframes) > 0 {
		if m.modelID != ModelGrokImagineVideo15 || isEdit || isExtension {
			details := fmt.Sprintf("xAI only supports keyframes with %q.", ModelGrokImagineVideo15)
			if m.modelID == ModelGrokImagineVideo15 {
				mode := "video editing"
				if isExtension {
					mode = "video extension"
				}
				details = fmt.Sprintf("xAI %s does not support keyframes.", mode)
			}
			warnings = append(warnings, unsupportedVideoWarning("keyframes", details))
		} else {
			keyframes := make([]map[string]interface{}, 0, len(provOpts.Keyframes))
			for _, kf := range provOpts.Keyframes {
				keyframes = append(keyframes, map[string]interface{}{
					"image":       map[string]interface{}{"url": kf.ImageURL},
					"timestamp_s": kf.TimestampSeconds,
				})
			}
			body["keyframes"] = keyframes
		}
	}

	// Video editing/extension: add source video URL. TS always sends
	// video: { url: xaiOptions!.videoUrl }, so an explicit mode without
	// videoUrl sends "video": {}.
	if isEdit || isExtension {
		video := map[string]interface{}{}
		if provOpts.VideoURL != nil {
			video["url"] = *provOpts.VideoURL
		}
		body["video"] = video
	}

	// Convert the start image (first_frame or legacy image-to-video input)
	// to the nested xAI request image object.
	startImage := resolveStartImage(opts)
	if startImage != nil {
		if isVideoFile(startImage) {
			feature := "image"
			if getFirstFrameImage(opts) != nil {
				feature = "frameImages"
			}
			warnings = append(warnings, unsupportedVideoWarning(feature,
				"xAI does not accept a video as a start/frame image. The video was ignored. "+
					`Use providerOptions.xai.mode "extend-video" to continue from a video instead.`))
		} else {
			body["image"] = map[string]interface{}{"url": fileToXAIURL(startImage)}
		}
	}

	// Only grok-imagine-video-1.5 supports a pinned last frame.
	if lastFrameImage := getLastFrameImage(opts); lastFrameImage != nil {
		if m.modelID != ModelGrokImagineVideo15 || isEdit || isExtension || isVideoFile(lastFrameImage) {
			details := fmt.Sprintf("xAI only supports last_frame with %q. The last frame was ignored.", ModelGrokImagineVideo15)
			if m.modelID == ModelGrokImagineVideo15 {
				details = "xAI only accepts an image last_frame for video generation. The last frame was ignored."
			}
			warnings = append(warnings, unsupportedVideoWarning("frameImages", details))
		} else {
			body["last_frame"] = map[string]interface{}{"url": fileToXAIURL(lastFrameImage)}
		}
	}

	// Reference images for R2V (reference-to-video) generation.
	if hasReferenceImages {
		referenceImages := resolveReferences(opts, provOpts, &warnings)
		referenceAudiosFromInputs := resolveReferenceAudiosFromInputs(opts)

		if referenceImages != nil {
			body["reference_images"] = referenceImages
		} else if len(referenceAudiosFromInputs) == 0 {
			// Explicit R2V with no usable image references would silently
			// send a plain generations request; tell the caller rather than
			// ever sending an empty `reference_images: []`.
			warnings = append(warnings, unsupportedVideoWarning("referenceImages",
				"xAI reference-to-video requires at least one image reference. The video will be generated without reference images."))
		}

		// validateVideoProviderOptions already rejects more than 3
		// voice ids with InvalidArgumentError, so every id here is used
		// as-is.
		referenceVoices := make([]map[string]interface{}, 0, len(provOpts.ReferenceVoiceIDs))
		for _, voiceID := range provOpts.ReferenceVoiceIDs {
			referenceVoices = append(referenceVoices, map[string]interface{}{"voice_id": voiceID})
		}
		referenceAudioInputs := append(append([]map[string]interface{}{}, referenceAudiosFromInputs...), referenceVoices...)
		if len(referenceAudioInputs) > 0 {
			if len(referenceAudioInputs) > 3 {
				warnings = append(warnings, unsupportedVideoWarning("inputReferences",
					"xAI reference-to-video supports at most 3 audio references. Only the first 3 were used."))
				referenceAudioInputs = referenceAudioInputs[:3]
			}
			body["reference_audios"] = referenceAudioInputs
		}

		// Reference-to-video is limited to 720p; downgrade a 1080p request.
		if res, ok := body["resolution"].(string); ok && res == "1080p" {
			warnings = append(warnings, unsupportedVideoWarning("resolution",
				"xAI reference-to-video is limited to 720p. The request was downgraded from 1080p to 720p."))
			body["resolution"] = "720p"
		}
	}

	// 1080p requires grok-imagine-video-1.5; the original grok-imagine-video
	// rejects it. Warn, but send the request as the caller asked.
	if res, ok := body["resolution"].(string); ok && res == "1080p" && m.modelID == ModelGrokImagineVideo {
		warnings = append(warnings, unsupportedVideoWarning("resolution",
			fmt.Sprintf("xAI model %q does not support 1080p. Use %q for 1080p, or a lower resolution. The request was sent with 1080p.",
				ModelGrokImagineVideo, ModelGrokImagineVideo15)))
	}

	// Warn when references were provided but cannot be used in the resolved
	// mode (e.g. alongside frameImages, in edit/extend modes, or when the
	// references carried no usable image or audio to drive
	// reference-to-video).
	if len(opts.InputReferences) > 0 && !hasReferenceImages {
		details := "xAI reference-to-video requires at least one image or audio reference. The references were ignored."
		if hasImageInputReference(opts) || hasAudioInputReference(opts) {
			details = "xAI only supports inputReferences for reference-to-video generation. The references were ignored."
		}
		warnings = append(warnings, unsupportedVideoWarning("inputReferences", details))
	}

	// Preset reference voices only apply to reference-to-video generation.
	if !hasReferenceImages && len(provOpts.ReferenceVoiceIDs) > 0 {
		warnings = append(warnings, unsupportedVideoWarning("referenceVoiceIds",
			"xAI only supports reference voices for reference-to-video generation. The reference voices were ignored."))
	}

	if !isExtension && provOpts.User != nil {
		body["user"] = *provOpts.User
	}

	// Passthrough any extra provider options not handled above.
	for k, v := range extra {
		body[k] = v
	}

	return body, warnings
}

// storageOptionsToWire converts XAIVideoStorageOptions to the xAI wire
// shape (snake_case, with publicUrl passed through as either a bool or
// {expires_after}).
func storageOptionsToWire(opts *XAIVideoStorageOptions) map[string]interface{} {
	wire := map[string]interface{}{"filename": opts.Filename}
	if opts.ExpiresAfter != nil {
		wire["expires_after"] = *opts.ExpiresAfter
	}
	switch v := opts.PublicURL.(type) {
	case bool:
		wire["public_url"] = v
	case map[string]interface{}:
		obj := map[string]interface{}{}
		if raw, ok := v["expiresAfter"]; ok {
			obj["expires_after"] = raw
		}
		wire["public_url"] = obj
	case *XAIVideoPublicURLOptions:
		if v != nil {
			obj := map[string]interface{}{}
			if v.ExpiresAfter != nil {
				obj["expires_after"] = *v.ExpiresAfter
			}
			wire["public_url"] = obj
		}
	case XAIVideoPublicURLOptions:
		obj := map[string]interface{}{}
		if v.ExpiresAfter != nil {
			obj["expires_after"] = *v.ExpiresAfter
		}
		wire["public_url"] = obj
	}
	return wire
}

// resolveReferenceImages resolves the reference images for R2V generation
// from the ReferenceImageURLs provider option. Empty entries are skipped so
// an empty `reference_images: []` is never sent.
func resolveReferenceImages(provOpts *XAIVideoProviderOptions) []map[string]interface{} {
	if len(provOpts.ReferenceImageURLs) == 0 {
		return nil
	}

	refs := make([]map[string]interface{}, 0, len(provOpts.ReferenceImageURLs))
	for _, url := range provOpts.ReferenceImageURLs {
		if url == "" {
			continue
		}
		refs = append(refs, map[string]interface{}{"url": url})
	}

	if len(refs) == 0 {
		return nil
	}
	return refs
}

// checkUnsupportedOptions checks for unsupported options and generates warnings
func (m *VideoModel) checkUnsupportedOptions(opts *provider.VideoModelV3CallOptions, provOpts *XAIVideoProviderOptions, mode string) []types.Warning {
	warnings := []types.Warning{}
	isEdit := mode == "edit-video"
	isExtension := mode == "extend-video"

	if opts.FPS != nil {
		warnings = append(warnings, unsupportedVideoWarning("fps", "xAI video models do not support custom FPS."))
	}

	if opts.Seed != nil {
		warnings = append(warnings, unsupportedVideoWarning("seed", "xAI video models do not support seed."))
	}

	if opts.N > 1 {
		warnings = append(warnings, unsupportedVideoWarning("n", "xAI video models do not support generating multiple videos per call. Only 1 video will be generated."))
	}

	if isEdit && opts.Duration != nil {
		warnings = append(warnings, unsupportedVideoWarning("duration", "xAI video editing does not support custom duration."))
	}

	if isEdit && opts.AspectRatio != "" {
		warnings = append(warnings, unsupportedVideoWarning("aspectRatio", "xAI video editing does not support custom aspect ratio."))
	}

	if isEdit && (provOpts.Resolution != nil || opts.Resolution != "") {
		warnings = append(warnings, unsupportedVideoWarning("resolution", "xAI video editing does not support custom resolution."))
	}

	if isExtension && opts.AspectRatio != "" {
		warnings = append(warnings, unsupportedVideoWarning("aspectRatio", "xAI video extension does not support custom aspect ratio."))
	}

	if isExtension && (provOpts.Resolution != nil || opts.Resolution != "") {
		warnings = append(warnings, unsupportedVideoWarning("resolution", "xAI video extension does not support custom resolution."))
	}

	if !isEdit && !isExtension && provOpts.Resolution == nil && opts.Resolution != "" && mapResolution(opts.Resolution) == "" {
		warnings = append(warnings, unsupportedVideoWarning(
			"resolution",
			fmt.Sprintf("Unrecognized resolution \"%s\". Use providerOptions.xai.resolution with \"480p\", \"720p\", or \"1080p\" instead.", opts.Resolution),
		))
	}

	return warnings
}

func unsupportedVideoWarning(feature, details string) types.Warning {
	return types.Warning{
		Type:    "unsupported",
		Feature: feature,
		Details: details,
		Message: details,
	}
}

func resolveMode(opts *provider.VideoModelV3CallOptions, provOpts *XAIVideoProviderOptions) string {
	if provOpts.Mode != nil && *provOpts.Mode != "" {
		return *provOpts.Mode
	}
	if provOpts.VideoURL != nil && *provOpts.VideoURL != "" {
		return "edit-video"
	}
	hasLegacyReferenceURLs := len(provOpts.ReferenceImageURLs) > 0
	// xAI supports image references, audio references, or both. Video-only
	// references must not flip a standard generation request into R2V.
	if hasImageInputReference(opts) || hasAudioInputReference(opts) || hasLegacyReferenceURLs {
		return "reference-to-video"
	}
	return ""
}

// topLevelMediaType returns the type before the "/" in a MIME media type
// (e.g. "video/mp4" -> "video"), or "" when mediaType is empty or has no
// "/".
func topLevelMediaType(mediaType string) string {
	idx := strings.IndexByte(mediaType, '/')
	if idx < 0 {
		return ""
	}
	return mediaType[:idx]
}

func isVideoFile(file *provider.VideoModelV3File) bool {
	return file != nil && file.MediaType != "" && topLevelMediaType(file.MediaType) == "video"
}

// isImageReference reports whether file should be treated as an image
// reference. References without a media type (only possible for URLs) are
// treated as images, matching the legacy ReferenceImageURLs behavior.
func isImageReference(file *provider.VideoModelV3File) bool {
	return file != nil && (file.MediaType == "" || topLevelMediaType(file.MediaType) == "image")
}

func isAudioReference(file *provider.VideoModelV3File) bool {
	return file != nil && file.MediaType != "" && topLevelMediaType(file.MediaType) == "audio"
}

// fileToXAIURL converts a VideoModelV3File to the URL xAI expects: the raw
// URL for a "url" file, or a base64 data: URL for a "file" (raw binary)
// file.
func fileToXAIURL(file *provider.VideoModelV3File) string {
	if file.Type == "url" {
		return file.URL
	}
	base64Data := base64.StdEncoding.EncodeToString(file.Data)
	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64Data)
}

func getFirstFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func getLastFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// resolveStartImage prefers a role-tagged first_frame image over the legacy
// Image field.
func resolveStartImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	if first := getFirstFrameImage(opts); first != nil {
		return first
	}
	return opts.Image
}

func hasImageInputReference(opts *provider.VideoModelV3CallOptions) bool {
	for i := range opts.InputReferences {
		if isImageReference(&opts.InputReferences[i]) {
			return true
		}
	}
	return false
}

func hasAudioInputReference(opts *provider.VideoModelV3CallOptions) bool {
	for i := range opts.InputReferences {
		if isAudioReference(&opts.InputReferences[i]) {
			return true
		}
	}
	return false
}

// resolveReferences resolves the reference images for R2V generation.
// First-class InputReferences win over the legacy ReferenceImageURLs
// provider option. Video references are not supported for
// reference-to-video and are skipped with a warning. Audio references are
// handled separately by resolveReferenceAudiosFromInputs.
func resolveReferences(opts *provider.VideoModelV3CallOptions, provOpts *XAIVideoProviderOptions, warnings *[]types.Warning) []map[string]interface{} {
	if len(opts.InputReferences) > 0 {
		var imageURLs []string
		for i := range opts.InputReferences {
			reference := &opts.InputReferences[i]
			if isAudioReference(reference) {
				continue
			}
			if !isImageReference(reference) {
				*warnings = append(*warnings, unsupportedVideoWarning("inputReferences",
					"xAI reference-to-video does not accept video references. The video reference was ignored. "+
						`Use providerOptions.xai.mode "extend-video" to continue from a video.`))
				continue
			}
			imageURLs = append(imageURLs, fileToXAIURL(reference))
		}
		if len(imageURLs) == 0 {
			return nil
		}
		refs := make([]map[string]interface{}, 0, len(imageURLs))
		for _, url := range imageURLs {
			refs = append(refs, map[string]interface{}{"url": url})
		}
		return refs
	}

	return resolveReferenceImages(provOpts)
}

// resolveReferenceAudiosFromInputs extracts audio references from
// InputReferences.
func resolveReferenceAudiosFromInputs(opts *provider.VideoModelV3CallOptions) []map[string]interface{} {
	var audios []map[string]interface{}
	for i := range opts.InputReferences {
		reference := &opts.InputReferences[i]
		if isAudioReference(reference) {
			audios = append(audios, map[string]interface{}{"url": fileToXAIURL(reference)})
		}
	}
	return audios
}

// mapResolution maps standard resolution strings to XAI format
func mapResolution(resolution string) string {
	resolutionMap := map[string]string{
		"1920x1080": "1080p",
		"1280x720":  "720p",
		"854x480":   "480p",
		"640x480":   "480p",
	}

	if mapped, ok := resolutionMap[resolution]; ok {
		return mapped
	}

	return ""
}

// extractVideoProviderOptions extracts XAI-specific provider options and any
// unrecognized keys (which are passed through to the API request body).
func extractVideoProviderOptions(opts map[string]interface{}) (*XAIVideoProviderOptions, map[string]interface{}, error) {
	provOpts, extra, err := parseVideoProviderOptions(opts)
	if err != nil {
		// TS 2e44144812: validation errors carry the options; with upload
		// delivery the signed URL must not reach diagnostics, so any option
		// error is reported without them.
		if raw, ok := opts["xai"]; ok && xaiVideoOptionsHaveOutput(raw) {
			return nil, nil, providererrors.NewArgumentError("providerOptions.xai", "Invalid xAI video provider options for upload delivery.", nil)
		}
	}
	var already *providererrors.InvalidArgumentError
	if errors.As(err, &already) {
		return nil, nil, err
	}
	if err != nil {
		// TS parseProviderOptions: any schema violation is an
		// InvalidArgumentError("invalid xai provider options").
		return nil, nil, providererrors.NewArgumentError("providerOptions", "invalid xai provider options: "+err.Error(), err)
	}
	return provOpts, extra, nil
}

func parseVideoProviderOptions(opts map[string]interface{}) (*XAIVideoProviderOptions, map[string]interface{}, error) {
	if opts == nil {
		return &XAIVideoProviderOptions{}, nil, nil
	}

	xaiRaw, ok := opts["xai"]
	if !ok || xaiRaw == nil {
		return &XAIVideoProviderOptions{}, nil, nil
	}

	// Convert to JSON and back to struct
	jsonData, err := json.Marshal(xaiRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal provider options: %w", err)
	}

	var provOpts XAIVideoProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal provider options: %w", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal provider options map: %w", err)
	}
	if err := resolveXAIVideoOutput(rawMap, &provOpts); err != nil {
		return nil, nil, err
	}
	if err := validateVideoProviderOptions(rawMap, &provOpts); err != nil {
		return nil, nil, err
	}

	// Collect any unrecognized keys for passthrough to the API.
	known := map[string]bool{
		"pollIntervalMs": true, "pollTimeoutMs": true,
		"resolution": true, "videoUrl": true,
		"mode": true, "referenceImageUrls": true,
		"referenceVoiceIds": true, "user": true,
		"keyframes": true, "storageOptions": true,
		"output": true, // TS 2e44144812: never passed through raw
	}
	extra := make(map[string]interface{})
	for k, v := range rawMap {
		if !known[k] {
			extra[k] = v
		}
	}

	return &provOpts, extra, nil
}

func validateVideoProviderOptions(rawMap map[string]interface{}, provOpts *XAIVideoProviderOptions) error {
	if provOpts.Mode != nil {
		switch *provOpts.Mode {
		case "edit-video", "extend-video", "reference-to-video":
		default:
			return fmt.Errorf("xai provider option mode must be one of edit-video, extend-video, reference-to-video")
		}
	}
	if provOpts.Resolution != nil {
		switch *provOpts.Resolution {
		case "480p", "720p", "1080p":
		default:
			return fmt.Errorf("xai provider option resolution must be one of 480p, 720p, 1080p")
		}
	}
	if provOpts.VideoURL != nil && *provOpts.VideoURL == "" {
		return fmt.Errorf("xai provider option videoUrl must not be empty")
	}
	if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs <= 0 {
		return fmt.Errorf("xai provider option pollIntervalMs must be positive")
	}
	if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs <= 0 {
		return fmt.Errorf("xai provider option pollTimeoutMs must be positive")
	}

	if _, ok := rawMap["referenceImageUrls"]; ok {
		if len(provOpts.ReferenceImageURLs) == 0 {
			return fmt.Errorf("xai provider option referenceImageUrls must contain at least 1 image")
		}
		if len(provOpts.ReferenceImageURLs) > 7 {
			return fmt.Errorf("xai provider option referenceImageUrls must contain at most 7 images")
		}
		for _, url := range provOpts.ReferenceImageURLs {
			if url == "" {
				return fmt.Errorf("xai provider option referenceImageUrls must not contain empty URLs")
			}
		}
	}

	if len(provOpts.ReferenceVoiceIDs) > 0 {
		// TS: z.array(nonEmptyStringSchema).max(3) -- more than 3 voice ids
		// is a hard validation error (InvalidArgumentError), not a warning
		// with silent truncation.
		if len(provOpts.ReferenceVoiceIDs) > 3 {
			return providererrors.NewArgumentError("referenceVoiceIds", "xai provider option referenceVoiceIds accepts at most 3 voice ids", nil)
		}
		for _, voiceID := range provOpts.ReferenceVoiceIDs {
			if voiceID == "" {
				return fmt.Errorf("xai provider option referenceVoiceIds must not contain empty ids")
			}
		}
	}

	if len(provOpts.Keyframes) > 4 {
		return providererrors.NewArgumentError("keyframes", "xai provider option keyframes accepts at most 4 entries", nil)
	}
	for _, kf := range provOpts.Keyframes {
		if kf.ImageURL == "" {
			return fmt.Errorf("xai provider option keyframes[].imageUrl must not be empty")
		}
		if kf.TimestampSeconds <= 0 {
			return fmt.Errorf("xai provider option keyframes[].timestampSeconds must be positive")
		}
	}

	if provOpts.StorageOptions != nil {
		if provOpts.StorageOptions.Filename == "" {
			return fmt.Errorf("xai provider option storageOptions.filename must not be empty")
		}
		if provOpts.StorageOptions.ExpiresAfter != nil {
			if *provOpts.StorageOptions.ExpiresAfter <= 0 || *provOpts.StorageOptions.ExpiresAfter > 2_592_000 {
				return fmt.Errorf("xai provider option storageOptions.expiresAfter must be between 1 and 2592000 seconds")
			}
		}
		if publicURLExpiresAfter, ok := publicURLObjectExpiresAfter(provOpts.StorageOptions.PublicURL); ok && publicURLExpiresAfter != nil {
			if *publicURLExpiresAfter < 3_600 || *publicURLExpiresAfter > 2_592_000 {
				return fmt.Errorf("xai provider option storageOptions.publicUrl.expiresAfter must be between 3600 and 2592000 seconds")
			}
		}
	}

	return nil
}

// publicURLObjectExpiresAfter extracts the expiresAfter field from a
// StorageOptions.PublicURL value when it is the object form (as opposed to
// a plain bool). ok is false when PublicURL is a bool or nil.
func publicURLObjectExpiresAfter(publicURL interface{}) (expiresAfter *int, ok bool) {
	switch v := publicURL.(type) {
	case map[string]interface{}:
		raw, present := v["expiresAfter"]
		if !present {
			return nil, true
		}
		f, isFloat := raw.(float64)
		if !isFloat {
			return nil, true
		}
		i := int(f)
		return &i, true
	case *XAIVideoPublicURLOptions:
		return v.ExpiresAfter, true
	case XAIVideoPublicURLOptions:
		return v.ExpiresAfter, true
	default:
		return nil, false
	}
}

// xaiVideoOptionsHaveOutput reports whether the raw xai options carry an
// output key (TS providerOptions.xai.output !== undefined).
func xaiVideoOptionsHaveOutput(raw interface{}) bool {
	if m, ok := raw.(map[string]interface{}); ok {
		_, has := m["output"]
		return has
	}
	if o, ok := raw.(*XAIVideoProviderOptions); ok {
		return o != nil && o.Output != nil
	}
	if o, ok := raw.(XAIVideoProviderOptions); ok {
		return o.Output != nil
	}
	return false
}

// resolveXAIVideoOutput reads and validates output: uploadUrl (TS
// z.url({protocol: /^https$/}) without username, password or hash), or the
// raw upload_url v0.5.2 forwarded (same validation; uploadUrl wins).
func resolveXAIVideoOutput(rawMap map[string]interface{}, provOpts *XAIVideoProviderOptions) error {
	raw, has := rawMap["output"]
	if !has {
		return nil
	}
	out, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("xai provider option output must be an object")
	}
	value, ok := out["uploadUrl"].(string)
	if _, present := out["uploadUrl"]; !present {
		value, ok = out["upload_url"].(string)
	}
	if !ok || !validXAIUploadURL(value) {
		return fmt.Errorf("xai provider option output.uploadUrl must be an https URL without credentials or fragment")
	}
	provOpts.Output = &XAIVideoOutput{UploadURL: value}
	return nil
}

func validXAIUploadURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.Fragment != "" {
		return false
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); u.User.Username() != "" || hasPassword {
			return false
		}
	}
	return true
}

// handleError converts provider errors
func (m *VideoModel) handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if provErr, ok := err.(*providererrors.ProviderError); ok {
		return provErr
	}
	if provErr, ok := xaiStatusError("xai", err); ok {
		return provErr // TS xaiFailedResponseHandler
	}
	return providererrors.NewProviderError("xai", 0, "", err.Error(), err)
}

// xaiVideoCreateResponse represents the video creation API response
type xaiVideoCreateResponse struct {
	RequestID string `json:"request_id"`
}

// xaiVideoStatusResponse represents the video status API response
type xaiVideoStatusResponse struct {
	// Status, the error fields and the video URL are pointers: TS tests
	// them with == null / ??, so an empty string is a value, not absence.
	Status   *string            `json:"status"`
	Video    *xaiVideoData      `json:"video,omitempty"`
	Model    string             `json:"model,omitempty"`
	Usage    *xaiVideoUsage     `json:"usage,omitempty"`
	Progress *float64           `json:"progress,omitempty"`
	Warnings []xaiWarning       `json:"warnings,omitempty"`
	Error    *xaiVideoStatusErr `json:"error,omitempty"`
}

// xaiVideoPending is the status of a 202 response TS reads as
// { status: 'pending' }.
var xaiVideoPending = "pending"

// is reports whether the response status is s.
func (r *xaiVideoStatusResponse) is(s string) bool {
	return r.Status != nil && *r.Status == s
}

// xaiVideoStatusErr holds the error details from a failed video status response.
type xaiVideoStatusErr struct {
	Code    *string `json:"code,omitempty"`
	Message *string `json:"message,omitempty"`
}

// xaiVideoUsage holds top-level usage data from the video status response.
type xaiVideoUsage struct {
	CostInUsdTicks *int64 `json:"cost_in_usd_ticks,omitempty"`
}

// xaiVideoData represents video data in the status response
type xaiVideoData struct {
	URL               *string             `json:"url"`
	Duration          *float64            `json:"duration,omitempty"`
	RespectModeration *bool               `json:"respect_moderation,omitempty"`
	FileOutput        *xaiVideoFileOutput `json:"file_output,omitempty"`
	StorageError      *string             `json:"storage_error,omitempty"`
}

// xaiVideoFileOutput describes a video persisted via storageOptions to the
// xAI Files API.
type xaiVideoFileOutput struct {
	FileID             string  `json:"file_id"`
	Filename           string  `json:"filename"`
	ExpiresAt          *int64  `json:"expires_at,omitempty"`
	PublicURL          *string `json:"public_url,omitempty"`
	PublicURLError     *string `json:"public_url_error,omitempty"`
	PublicURLExpiresAt *int64  `json:"public_url_expires_at,omitempty"`
}

// resolveVideoURL mirrors TS `video?.url ?? video?.file_output?.public_url
// ?? undefined`: the direct video URL wins, falling back to the persisted
// Files API public URL when storageOptions was used.
func resolveVideoURL(video *xaiVideoData) string {
	if video == nil {
		return ""
	}
	if video.URL != nil {
		return *video.URL
	}
	if video.FileOutput != nil && video.FileOutput.PublicURL != nil {
		return *video.FileOutput.PublicURL
	}
	return ""
}

type xaiWarning struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
