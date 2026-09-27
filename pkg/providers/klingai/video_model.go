package klingai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for KlingAI
type VideoModel struct {
	prov    *Provider
	modelID string
	mode    VideoMode
}

// newVideoModel creates a new KlingAI video generation model
func newVideoModel(prov *Provider, modelID string) (*VideoModel, error) {
	mode, err := detectMode(modelID)
	if err != nil {
		return nil, err
	}

	return &VideoModel{
		prov:    prov,
		modelID: modelID,
		mode:    mode,
	}, nil
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "klingai.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns 1 (KlingAI generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// klingaiOperation is the opaque operation reference returned by DoStart and
// passed back into DoStatus. endpointPath is carried alongside taskId
// because the status URL depends on which mode endpoint the task was
// created against (text2video/image2video/multi-image2video/motion-control).
type klingaiOperation struct {
	TaskID       string `json:"taskId"`
	EndpointPath string `json:"endpointPath"`
}

// DoStart starts an asynchronous video generation via the mode-appropriate
// KlingAI endpoint and returns an opaque operation reference
// (TS KlingAIVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	provOpts, err := extractProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	body, warnings, endpointPath, err := m.buildRequestBody(&opts.VideoModelV3CallOptions, provOpts)
	if err != nil {
		return nil, err
	}

	warnings = append(warnings, m.checkUnsupportedOptions(&opts.VideoModelV3CallOptions)...)

	// Progress notifications require a protocol-aware receiver, so we don't
	// implement VideoModelWebhookHandler; forward webhookUrl as callback_url
	// for a caller-owned receiver that filters progress notifications.
	if opts.WebhookURL != "" {
		body["callback_url"] = opts.WebhookURL
	}

	authToken, err := m.prov.GenerateAuthToken()
	if err != nil {
		return nil, NewAuthError(err.Error())
	}

	submitResp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    endpointPath,
		Body:    body,
		Headers: mergeKlingAIHeaders(authToken, opts.Headers),
	})
	if err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to submit request: %v", err))
	}
	if submitResp.StatusCode != 200 {
		return nil, parseKlingAIErrorBody(submitResp.StatusCode, submitResp.Body)
	}

	var createResp createTaskResponse
	if err := json.Unmarshal(submitResp.Body, &createResp); err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to parse response: %v", err))
	}
	if createResp.Code != 0 {
		return nil, NewError(createResp.Code, createResp.Message, "")
	}
	if createResp.Data == nil || createResp.Data.TaskID == "" {
		return nil, NewVideoGenerationError("no task ID in response")
	}

	operation, _ := json.Marshal(klingaiOperation{TaskID: createResp.Data.TaskID, EndpointPath: endpointPath})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   convertHTTPHeaders(submitResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via the task's status endpoint (TS KlingAIVideoModel#doStatus).
// The status URL is fetched through fileutil's validated-redirect poller,
// matching TS's `getFromApi({ validateUrl: true, trustedOrigin })`: any
// redirect away from the provider's own base URL is SSRF-validated and
// dialed through a DNS-pinning transport, and credentials are only
// forwarded on the trusted hop.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	var op klingaiOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("klingai: invalid operation reference: %w", err)
	}

	authToken, err := m.prov.GenerateAuthToken()
	if err != nil {
		return nil, NewAuthError(err.Error())
	}

	downloadOpts := fileutil.TrustedOriginDownloadOptions(m.prov.config.BaseURL, nil)
	downloadOpts.Headers = mergeKlingAIHeaders(authToken, opts.Headers)

	statusURL := strings.TrimRight(m.prov.config.BaseURL, "/") + op.EndpointPath + "/" + op.TaskID

	var statusResp taskStatusResponse
	result, err := fileutil.PollJSON(ctx, statusURL, downloadOpts, &statusResp)
	if err != nil {
		return nil, m.handlePollError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: time.Now(),
		ModelID:   m.modelID,
		Headers:   flattenKlingAIHeaders(result.Headers),
	}

	if statusResp.Code != 0 {
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    statusResp.Message,
			Response: responseInfo,
		}, nil
	}
	if statusResp.Data == nil {
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    "no data in status response",
			Response: responseInfo,
		}, nil
	}

	switch statusResp.Data.TaskStatus {
	case "succeed":
		return m.buildCompletedStatusResult(&statusResp, op.TaskID, responseInfo)

	case "failed":
		msg := statusResp.Data.TaskStatusMsg
		if msg == "" {
			msg = "Unknown error"
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    fmt.Sprintf("Video generation failed: %s", msg),
			Response: responseInfo,
		}, nil

	default:
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusPending,
			Response: responseInfo,
		}, nil
	}
}

// buildCompletedStatusResult converts a succeeded task status response into
// a completed operation result, returning an error (mirroring TS's thrown
// AISDKError from buildCompletedResult) when the task carries no videos or
// no valid video URLs at all.
func (m *VideoModel) buildCompletedStatusResult(resp *taskStatusResponse, taskID string, responseInfo provider.VideoModelV3ResponseInfo) (*provider.VideoModelV3OperationStatusResult, error) {
	if resp.Data == nil || resp.Data.TaskResult == nil || len(resp.Data.TaskResult.Videos) == 0 {
		return nil, NewVideoGenerationError("No videos were returned in the response.")
	}

	videos := []provider.VideoModelV3VideoData{}
	videoMetadata := []map[string]interface{}{}

	for _, video := range resp.Data.TaskResult.Videos {
		if video.URL == "" {
			continue
		}
		videos = append(videos, provider.VideoModelV3VideoData{Type: "url", URL: video.URL, MediaType: "video/mp4"})
		metadata := map[string]interface{}{"id": video.ID, "url": video.URL}
		if video.WatermarkURL != "" {
			metadata["watermarkUrl"] = video.WatermarkURL
		}
		if video.Duration != "" {
			metadata["duration"] = video.Duration
		}
		videoMetadata = append(videoMetadata, metadata)
	}

	if len(videos) == 0 {
		return nil, NewVideoGenerationError("No valid video URLs in response.")
	}

	return &provider.VideoModelV3OperationStatusResult{
		Status: provider.VideoOperationStatusCompleted,
		Videos: videos,
		ProviderMetadata: map[string]interface{}{
			"klingai": map[string]interface{}{"taskId": taskID, "videos": videoMetadata},
		},
		Response: responseInfo,
	}, nil
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
	pollOpts := m.getPollOptions(provOpts)

	var finalStatus *provider.VideoModelV3OperationStatusResult
	var jobFailureErr error

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.Headers,
		})
		if err != nil {
			// A genuine DoStatus error (transport failure, malformed
			// response, or a succeeded task with no valid videos) — not a
			// job-status transition. Propagated as-is below.
			return nil, err
		}
		switch status.Status {
		case provider.VideoOperationStatusCompleted:
			finalStatus = status
			return &polling.JobResult{Status: polling.JobStatusCompleted}, nil
		case provider.VideoOperationStatusError:
			jobFailureErr = NewVideoGenerationFailedError(status.Error)
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
		if strings.Contains(pollErr.Error(), "polling timeout") || strings.Contains(pollErr.Error(), "max polling attempts") {
			return nil, NewTimeoutError(fmt.Sprintf("%dms", pollOpts.PollTimeoutMs))
		}
		if strings.Contains(pollErr.Error(), "status check failed") {
			return nil, pollErr
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

// handlePollError converts a fileutil poll error (a providererrors.DownloadError
// for a non-2xx HTTP response, or a validation/network error) into a
// KlingAI API error where possible.
func (m *VideoModel) handlePollError(err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		return parseKlingAIErrorBody(dlErr.StatusCode, dlErr.Body)
	}
	return fmt.Errorf("failed to check status: %w", err)
}

// parseKlingAIErrorBody decodes a non-2xx KlingAI response body into the
// {code, message} error envelope (TS klingaiFailedResponseHandler /
// klingaiErrorDataSchema), falling back to a generic error carrying the raw
// body when the envelope cannot be parsed or carries no message.
func parseKlingAIErrorBody(statusCode int, body []byte) error {
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if jsonErr := json.Unmarshal(body, &envelope); jsonErr == nil && envelope.Message != "" {
		return NewError(envelope.Code, envelope.Message, "")
	}
	return NewVideoGenerationError(fmt.Sprintf("API returned status %d: %s", statusCode, string(body)))
}

// detectMode detects the video generation mode from the model ID suffix
func detectMode(modelID string) (VideoMode, error) {
	if strings.HasSuffix(modelID, "-t2v") {
		return VideoModeT2V, nil
	}
	if strings.HasSuffix(modelID, "-i2v") {
		return VideoModeI2V, nil
	}
	if strings.HasSuffix(modelID, "-motion-control") {
		return VideoModeMotionControl, nil
	}
	return "", fmt.Errorf("unsupported model ID: %s (must end with -t2v, -i2v, or -motion-control)", modelID)
}

// endpointForMode returns the API endpoint path for a (possibly effective,
// e.g. mi2v) mode.
func (m *VideoModel) endpointForMode(mode VideoMode) string {
	switch mode {
	case VideoModeT2V:
		return "/v1/videos/text2video"
	case VideoModeI2V:
		return "/v1/videos/image2video"
	case VideoModeMultiImage:
		return "/v1/videos/multi-image2video"
	case VideoModeMotionControl:
		return "/v1/videos/motion-control"
	default:
		return ""
	}
}

// getEndpoint returns the API endpoint for this model's declared (not
// reference-image-elevated) mode.
func (m *VideoModel) getEndpoint() string {
	return m.endpointForMode(m.mode)
}

// getAPIModelName derives the KlingAI API model_name from the SDK model ID.
// Strips the mode suffix, removes trailing ".0" version suffixes, then converts
// remaining dots to hyphens.
// Examples:
//   - 'kling-v2.6-t2v' → 'kling-v2-6'
//   - 'kling-v2.1-master-i2v' → 'kling-v2-1-master'
//   - 'kling-v3.0-t2v' → 'kling-v3'
//   - 'kling-v3.0-i2v' → 'kling-v3'
func (m *VideoModel) getAPIModelName() string {
	var suffix string
	switch m.mode {
	case VideoModeMotionControl:
		suffix = "-motion-control"
	default:
		suffix = "-" + string(m.mode)
	}

	baseName := strings.TrimSuffix(m.modelID, suffix)
	// Strip trailing ".0" version suffix before replacing dots with hyphens.
	// This ensures "kling-v3.0" maps to "kling-v3" rather than "kling-v3-0".
	baseName = strings.TrimSuffix(baseName, ".0")
	return strings.ReplaceAll(baseName, ".", "-")
}

// IsImageToVideo returns true if this model performs image-to-video generation.
func (m *VideoModel) IsImageToVideo() bool {
	return m.mode == VideoModeI2V
}

// klingaiIsVideoFile reports whether a file's top-level media type is
// "video" (KlingAI does not accept video as a frame image or reference).
func klingaiIsVideoFile(f *provider.VideoModelV3File) bool {
	return f.MediaType != "" && strings.HasPrefix(f.MediaType, "video/")
}

func klingaiGetFirstFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func klingaiGetLastFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// klingaiResolveStartImage resolves the start (first) frame image, preferring
// a frameImages first_frame over the legacy top-level image, and rejecting
// (with a warning) a video file (TS resolveStartImage).
func klingaiResolveStartImage(opts *provider.VideoModelV3CallOptions, warnings *[]types.Warning) *provider.VideoModelV3File {
	startImage := klingaiGetFirstFrameImage(opts)
	if startImage == nil {
		startImage = opts.Image
	}
	if startImage == nil {
		return nil
	}
	if klingaiIsVideoFile(startImage) {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "frameImages",
			Details: "KlingAI does not accept video as a frame image; it was ignored.",
		})
		return nil
	}
	return startImage
}

// klingaiResolveImageTail resolves the end (last) frame image URL/data URI,
// preferring a frameImages last_frame over the legacy imageTail provider
// option, and rejecting (with a warning) a video file (TS resolveImageTail).
func klingaiResolveImageTail(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions, warnings *[]types.Warning) *string {
	lastFrame := klingaiGetLastFrameImage(opts)
	if lastFrame != nil {
		if klingaiIsVideoFile(lastFrame) {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "frameImages",
				Details: "KlingAI does not accept video as a frame image; it was ignored.",
			})
			return nil
		}
		encoded, err := klingaiEncodeImage(lastFrame)
		if err != nil {
			return nil
		}
		return &encoded
	}
	return provOpts.ImageTail
}

// klingaiGetReferenceImages resolves inputReferences into reference-to-video
// image inputs, dropping (with a warning) any video reference — KlingAI does
// not support video reference inputs — and returning nil when frameImages
// were also supplied (TS getReferenceImages).
func klingaiGetReferenceImages(opts *provider.VideoModelV3CallOptions, warnings *[]types.Warning) []provider.VideoModelV3File {
	if len(opts.FrameImages) > 0 {
		return nil
	}
	if len(opts.InputReferences) == 0 {
		return nil
	}

	imageReferences := make([]provider.VideoModelV3File, 0, len(opts.InputReferences))
	for i := range opts.InputReferences {
		reference := opts.InputReferences[i]
		if klingaiIsVideoFile(&reference) {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "inputReferences",
				Details: "KlingAI does not support video reference inputs; the video reference was ignored.",
			})
			continue
		}
		imageReferences = append(imageReferences, reference)
	}

	if len(imageReferences) == 0 {
		return nil
	}
	return imageReferences
}

// buildRequestBody builds the API request body, routing to the appropriate
// mode builder (elevating i2v to mi2v when inputReferences are supplied) and
// returning the resolved endpoint path alongside the body and warnings.
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, []types.Warning, string, error) {
	warnings := []types.Warning{}

	referenceImages := klingaiGetReferenceImages(opts, &warnings)
	effectiveMode := m.mode
	if m.mode == VideoModeI2V && referenceImages != nil {
		effectiveMode = VideoModeMultiImage
	}

	var body map[string]interface{}
	var bodyWarnings []types.Warning
	var err error

	switch effectiveMode {
	case VideoModeMotionControl:
		body, bodyWarnings, err = m.buildMotionControlBody(opts, provOpts)
	case VideoModeT2V:
		body, bodyWarnings, err = m.buildT2VBody(opts, provOpts)
	case VideoModeMultiImage:
		body, bodyWarnings, err = m.buildMultiImageBody(opts, provOpts, referenceImages)
	default:
		body, bodyWarnings, err = m.buildI2VBody(opts, provOpts)
	}
	if err != nil {
		return nil, nil, "", err
	}
	warnings = append(warnings, bodyWarnings...)

	if referenceImages != nil && effectiveMode != VideoModeMultiImage {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "inputReferences",
			Details: "KlingAI only supports inputReferences (reference-to-video) on image-to-video models. The reference images were ignored.",
		})
	}

	return body, warnings, m.endpointForMode(effectiveMode), nil
}

// klingaiGenerateAudioSound maps the standard generateAudio call option to
// KlingAI's "on"/"off" sound string, falling back to the provider option
// when generateAudio was not set (TS: generateAudio != null ? (generateAudio
// ? 'on' : 'off') : klingaiOptions?.sound).
func klingaiGenerateAudioSound(generateAudio *bool, sound *string) *string {
	if generateAudio == nil {
		return sound
	}
	s := "off"
	if *generateAudio {
		s = "on"
	}
	return &s
}

// buildT2VBody builds the request body for text-to-video
func (m *VideoModel) buildT2VBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, []types.Warning, error) {
	body := map[string]interface{}{
		"model_name": m.getAPIModelName(),
	}
	warnings := []types.Warning{}

	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	if provOpts.NegativePrompt != nil {
		body["negative_prompt"] = *provOpts.NegativePrompt
	}

	if sound := klingaiGenerateAudioSound(opts.GenerateAudio, provOpts.Sound); sound != nil {
		body["sound"] = *sound
	}

	if provOpts.CfgScale != nil {
		body["cfg_scale"] = *provOpts.CfgScale
	}

	if provOpts.Mode != nil {
		body["mode"] = *provOpts.Mode
	}

	if provOpts.CameraControl != nil {
		body["camera_control"] = provOpts.CameraControl
	}

	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	if opts.Duration != nil {
		body["duration"] = strconv.FormatFloat(*opts.Duration, 'f', -1, 64)
	}

	// v3.0 multi-shot
	if provOpts.MultiShot != nil {
		body["multi_shot"] = *provOpts.MultiShot
	}

	if provOpts.ShotType != nil {
		body["shot_type"] = *provOpts.ShotType
	}

	if len(provOpts.MultiPrompt) > 0 {
		body["multi_prompt"] = provOpts.MultiPrompt
	}

	// v3.0 voice control
	if len(provOpts.VoiceList) > 0 {
		body["voice_list"] = provOpts.VoiceList
	}

	if provOpts.WatermarkEnabled != nil {
		body["watermark_info"] = map[string]bool{"enabled": *provOpts.WatermarkEnabled}
	}

	// Image is not supported for T2V (checked via resolveStartImage so a
	// frameImages first_frame is caught too, not just the legacy field).
	if klingaiResolveStartImage(opts, &warnings) != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "image",
			Details: "KlingAI text-to-video does not support image input. Use an image-to-video model instead.",
		})
	}

	// Add passthrough options
	if provOpts.Additional != nil {
		for k, v := range provOpts.Additional {
			body[k] = v
		}
	}

	return body, warnings, nil
}

// buildI2VBody builds the request body for image-to-video
func (m *VideoModel) buildI2VBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, []types.Warning, error) {
	body := map[string]interface{}{
		"model_name": m.getAPIModelName(),
	}
	warnings := []types.Warning{}

	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	// Handle start frame image: a frameImages first_frame takes precedence
	// over the legacy top-level image.
	if startImage := klingaiResolveStartImage(opts, &warnings); startImage != nil {
		imageData, err := klingaiEncodeImage(startImage)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode image: %w", err)
		}
		body["image"] = imageData
	}

	// End frame image: prefer top-level frameImages (last_frame), fall back
	// to providerOptions.klingai.imageTail.
	if imageTail := klingaiResolveImageTail(opts, provOpts, &warnings); imageTail != nil {
		body["image_tail"] = *imageTail
	}

	if provOpts.NegativePrompt != nil {
		body["negative_prompt"] = *provOpts.NegativePrompt
	}

	if sound := klingaiGenerateAudioSound(opts.GenerateAudio, provOpts.Sound); sound != nil {
		body["sound"] = *sound
	}

	if provOpts.CfgScale != nil {
		body["cfg_scale"] = *provOpts.CfgScale
	}

	if provOpts.Mode != nil {
		body["mode"] = *provOpts.Mode
	}

	if provOpts.CameraControl != nil {
		body["camera_control"] = provOpts.CameraControl
	}

	if provOpts.StaticMask != nil {
		body["static_mask"] = *provOpts.StaticMask
	}

	if len(provOpts.DynamicMasks) > 0 {
		body["dynamic_masks"] = provOpts.DynamicMasks
	}

	if opts.Duration != nil {
		body["duration"] = strconv.FormatFloat(*opts.Duration, 'f', -1, 64)
	}

	// v3.0 multi-shot
	if provOpts.MultiShot != nil {
		body["multi_shot"] = *provOpts.MultiShot
	}

	if provOpts.ShotType != nil {
		body["shot_type"] = *provOpts.ShotType
	}

	if len(provOpts.MultiPrompt) > 0 {
		body["multi_prompt"] = provOpts.MultiPrompt
	}

	// v3.0 element control (I2V only)
	if len(provOpts.ElementList) > 0 {
		body["element_list"] = provOpts.ElementList
	}

	// v3.0 voice control
	if len(provOpts.VoiceList) > 0 {
		body["voice_list"] = provOpts.VoiceList
	}

	if provOpts.WatermarkEnabled != nil {
		body["watermark_info"] = map[string]bool{"enabled": *provOpts.WatermarkEnabled}
	}

	// AspectRatio is not supported for I2V (determined by input image)
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "KlingAI image-to-video does not support aspectRatio. The output dimensions are determined by the input image.",
		})
	}

	// Add passthrough options
	if provOpts.Additional != nil {
		for k, v := range provOpts.Additional {
			body[k] = v
		}
	}

	return body, warnings, nil
}

// buildMultiImageBody builds the request body for reference-to-video
// (multi-image2video), driven by inputReferences (TS buildMultiImageBody).
func (m *VideoModel) buildMultiImageBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions, referenceImages []provider.VideoModelV3File) (map[string]interface{}, []types.Warning, error) {
	imageList := make([]map[string]interface{}, 0, len(referenceImages))
	for i := range referenceImages {
		encoded, err := klingaiEncodeImage(&referenceImages[i])
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode reference image: %w", err)
		}
		imageList = append(imageList, map[string]interface{}{"image": encoded})
	}

	body := map[string]interface{}{
		// mi2v reuses the i2v model's own id-derived name (TS
		// getApiModelName(modelId, 'mi2v') uses the same "-i2v" suffix).
		"model_name": m.getAPIModelName(),
		"image_list": imageList,
	}
	warnings := []types.Warning{}

	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	if provOpts.NegativePrompt != nil {
		body["negative_prompt"] = *provOpts.NegativePrompt
	}

	if provOpts.CfgScale != nil {
		body["cfg_scale"] = *provOpts.CfgScale
	}

	if provOpts.Mode != nil {
		body["mode"] = *provOpts.Mode
	}

	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	if opts.Duration != nil {
		body["duration"] = strconv.FormatFloat(*opts.Duration, 'f', -1, 64)
	}

	if provOpts.WatermarkEnabled != nil {
		body["watermark_info"] = map[string]bool{"enabled": *provOpts.WatermarkEnabled}
	}

	if klingaiResolveStartImage(opts, &warnings) != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "image",
			Details: "KlingAI reference-to-video does not support a separate start frame. Provide all guidance images via inputReferences instead.",
		})
	}

	if klingaiResolveImageTail(opts, provOpts, &warnings) != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "frameImages",
			Details: "KlingAI reference-to-video does not support a last frame (image_tail). Provide all guidance images via inputReferences instead.",
		})
	}

	if provOpts.Additional != nil {
		for k, v := range provOpts.Additional {
			body[k] = v
		}
	}

	return body, warnings, nil
}

// buildMotionControlBody builds the request body for motion control
func (m *VideoModel) buildMotionControlBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, []types.Warning, error) {
	warnings := []types.Warning{}

	// Validate required options
	if provOpts.VideoUrl == nil || *provOpts.VideoUrl == "" {
		return nil, nil, NewMissingVideoOptionsError("videoUrl")
	}
	if provOpts.CharacterOrientation == nil || *provOpts.CharacterOrientation == "" {
		return nil, nil, NewMissingVideoOptionsError("characterOrientation")
	}
	if provOpts.Mode == nil || *provOpts.Mode == "" {
		return nil, nil, NewMissingVideoOptionsError("mode")
	}

	body := map[string]interface{}{
		"model_name":            m.getAPIModelName(),
		"video_url":             *provOpts.VideoUrl,
		"character_orientation": *provOpts.CharacterOrientation,
		"mode":                  *provOpts.Mode,
	}

	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	// Handle image for motion control
	if startImage := klingaiResolveStartImage(opts, &warnings); startImage != nil {
		imageData, err := klingaiEncodeImage(startImage)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode image: %w", err)
		}
		body["image_url"] = imageData
	}

	if provOpts.KeepOriginalSound != nil {
		body["keep_original_sound"] = *provOpts.KeepOriginalSound
	}

	if provOpts.WatermarkEnabled != nil {
		body["watermark_info"] = map[string]bool{
			"enabled": *provOpts.WatermarkEnabled,
		}
	}

	// v3.0 element control
	if len(provOpts.ElementList) > 0 {
		body["element_list"] = provOpts.ElementList
	}

	// AspectRatio and duration not supported for motion control
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "KlingAI Motion Control does not support aspectRatio. The output dimensions are determined by the reference image/video.",
		})
	}

	if opts.Duration != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "duration",
			Details: "KlingAI Motion Control does not support custom duration. The output duration matches the reference video duration.",
		})
	}

	// Add passthrough options
	if provOpts.Additional != nil {
		for k, v := range provOpts.Additional {
			body[k] = v
		}
	}

	return body, warnings, nil
}

// klingaiEncodeImage encodes an image file to the format expected by KlingAI
func klingaiEncodeImage(img *provider.VideoModelV3File) (string, error) {
	if img.Type == "url" {
		return img.URL, nil
	}

	// For binary data, encode as base64
	if img.Type == "file" {
		return base64.StdEncoding.EncodeToString(img.Data), nil
	}

	return "", fmt.Errorf("unsupported image type: %s", img.Type)
}

// encodeImage encodes an image file to the format expected by KlingAI
// (method form retained for source compatibility; delegates to klingaiEncodeImage).
func (m *VideoModel) encodeImage(img *provider.VideoModelV3File) (string, error) {
	return klingaiEncodeImage(img)
}

// checkUnsupportedOptions checks for universally unsupported standard options
func (m *VideoModel) checkUnsupportedOptions(opts *provider.VideoModelV3CallOptions) []types.Warning {
	warnings := []types.Warning{}

	if opts.Resolution != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "resolution",
			Details: "KlingAI video models do not support the resolution option.",
		})
	}

	// TS checks `if (options.seed)` / `if (options.fps)`: a truthy check, so
	// an explicit zero (like an omitted value) does not warn.
	if opts.Seed != nil && *opts.Seed != 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
			Details: "KlingAI video models do not support seed for deterministic generation.",
		})
	}

	if opts.FPS != nil && *opts.FPS != 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "fps",
			Details: "KlingAI video models do not support custom FPS.",
		})
	}

	if opts.N > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "n",
			Details: "KlingAI video models do not support generating multiple videos per call. Only 1 video will be generated.",
		})
	}

	return warnings
}

// getPollOptions extracts polling options from provider options
func (m *VideoModel) getPollOptions(provOpts *ProviderOptions) polling.PollOptions {
	opts := polling.DefaultPollOptions()

	// Override defaults
	opts.PollIntervalMs = 5000  // 5 seconds
	opts.PollTimeoutMs = 600000 // 10 minutes

	if provOpts.PollIntervalMs != nil {
		opts.PollIntervalMs = *provOpts.PollIntervalMs
	}

	if provOpts.PollTimeoutMs != nil {
		opts.PollTimeoutMs = *provOpts.PollTimeoutMs
	}

	return opts
}

// convertHTTPHeaders flattens net/http.Header (map[string][]string) into the
// map[string]string expected by VideoModelV3ResponseInfo.Headers, taking the
// first value for each header key (matching the TS SDK's single-value behavior).
func convertHTTPHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

// flattenKlingAIHeaders flattens a fileutil.DownloadResult's headers
// (map[string][]string) the same way convertHTTPHeaders does for
// net/http.Header.
func flattenKlingAIHeaders(h map[string][]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// mergeKlingAIHeaders builds the request headers for a KlingAI call: a fresh
// bearer token followed by any caller-supplied headers.
func mergeKlingAIHeaders(authToken string, headers map[string]string) map[string]string {
	out := map[string]string{"Authorization": "Bearer " + authToken}
	for k, v := range headers {
		out[k] = v
	}
	return out
}

// handledProviderOptionKeys is the set of JSON keys that ProviderOptions handles directly.
// Any keys not in this set are treated as passthrough options (stored in Additional).
var handledProviderOptionKeys = map[string]bool{
	"mode": true, "pollIntervalMs": true, "pollTimeoutMs": true,
	"negativePrompt": true, "sound": true, "cfgScale": true, "cameraControl": true,
	"multiShot": true, "shotType": true, "multiPrompt": true, "voiceList": true,
	"imageTail": true, "staticMask": true, "dynamicMasks": true,
	"elementList": true, "videoUrl": true, "characterOrientation": true,
	"keepOriginalSound": true, "watermarkEnabled": true,
}

// extractProviderOptions extracts KlingAI provider options from the generic options map.
// Unknown keys are captured into ProviderOptions.Additional for passthrough to the API body,
// matching the TS SDK addPassthroughOptions behavior.
func extractProviderOptions(opts map[string]interface{}) (*ProviderOptions, error) {
	if opts == nil {
		return &ProviderOptions{}, nil
	}

	klingaiOpts, ok := opts["klingai"]
	if !ok {
		return &ProviderOptions{}, nil
	}

	// Marshal to JSON for typed unmarshaling of known fields
	jsonData, err := json.Marshal(klingaiOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal provider options: %w", err)
	}

	var provOpts ProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider options: %w", err)
	}

	// Capture unknown keys as passthrough options (Additional)
	var allKeys map[string]interface{}
	if err := json.Unmarshal(jsonData, &allKeys); err == nil {
		for k, v := range allKeys {
			if !handledProviderOptionKeys[k] {
				if provOpts.Additional == nil {
					provOpts.Additional = make(map[string]interface{})
				}
				provOpts.Additional[k] = v
			}
		}
	}

	return &provOpts, nil
}
