package klingai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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
	return "v3"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "klingai.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (KlingAI generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	return nil // Default to 1
}

// DoGenerate performs video generation with polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Capture timestamp before any I/O, matching TS SDK's currentDate capture
	startTime := time.Now()

	// Extract provider options
	provOpts, err := extractProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	// Build request body based on mode
	body, warnings, err := m.buildRequestBody(opts, provOpts)
	if err != nil {
		return nil, err
	}

	// Add warnings for unsupported standard options
	warnings = append(warnings, m.checkUnsupportedOptions(opts)...)

	// Get authentication token
	authToken, err := m.prov.GenerateAuthToken()
	if err != nil {
		return nil, NewAuthError(err.Error())
	}

	// Submit generation request with auth header
	endpoint := m.getEndpoint()
	submitResp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    endpoint,
		Body:    body,
		Headers: map[string]string{"Authorization": "Bearer " + authToken},
	})
	if err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to submit request: %v", err))
	}

	if submitResp.StatusCode != 200 {
		return nil, NewVideoGenerationError(fmt.Sprintf("API returned status %d: %s", submitResp.StatusCode, string(submitResp.Body)))
	}

	// Parse submission response
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

	taskID := createResp.Data.TaskID

	// Get polling options
	pollOpts := m.getPollOptions(provOpts)

	// Poll for completion; capture response headers from the final poll
	statusResp, responseHeaders, err := m.pollForCompletion(ctx, authToken, endpoint, taskID, pollOpts)
	if err != nil {
		return nil, err
	}

	// Check that the task returned at least one video (first TS empty-check)
	if statusResp.Data == nil || statusResp.Data.TaskResult == nil || len(statusResp.Data.TaskResult.Videos) == 0 {
		return nil, NewVideoGenerationError("No videos were returned in the response.")
	}

	// Convert response to SDK format
	result := m.convertResponse(statusResp, taskID, warnings, startTime, responseHeaders)

	// Second TS check: all returned videos had empty URLs
	if len(result.Videos) == 0 {
		return nil, NewVideoGenerationError("No valid video URLs in response.")
	}

	return result, nil
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

// getEndpoint returns the API endpoint for this model's mode
func (m *VideoModel) getEndpoint() string {
	switch m.mode {
	case VideoModeT2V:
		return "/v1/videos/text2video"
	case VideoModeI2V:
		return "/v1/videos/image2video"
	case VideoModeMotionControl:
		return "/v1/videos/motion-control"
	default:
		return ""
	}
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

// buildRequestBody builds the API request body based on the mode
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, []types.Warning, error) {
	switch m.mode {
	case VideoModeT2V:
		return m.buildT2VBody(opts, provOpts)
	case VideoModeI2V:
		return m.buildI2VBody(opts, provOpts)
	case VideoModeMotionControl:
		return m.buildMotionControlBody(opts, provOpts)
	default:
		return nil, nil, fmt.Errorf("unsupported mode: %s", m.mode)
	}
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

	if provOpts.Sound != nil {
		body["sound"] = *provOpts.Sound
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

	// Image is not supported for T2V
	if opts.Image != nil {
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

	// Handle start frame image
	if opts.Image != nil {
		imageData, err := m.encodeImage(opts.Image)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode image: %w", err)
		}
		body["image"] = imageData
	}

	if provOpts.ImageTail != nil {
		body["image_tail"] = *provOpts.ImageTail
	}

	if provOpts.NegativePrompt != nil {
		body["negative_prompt"] = *provOpts.NegativePrompt
	}

	if provOpts.Sound != nil {
		body["sound"] = *provOpts.Sound
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
	if opts.Image != nil {
		imageData, err := m.encodeImage(opts.Image)
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

// encodeImage encodes an image file to the format expected by KlingAI
func (m *VideoModel) encodeImage(img *provider.VideoModelV3File) (string, error) {
	if img.Type == "url" {
		return img.URL, nil
	}

	// For binary data, encode as base64
	if img.Type == "file" {
		return base64.StdEncoding.EncodeToString(img.Data), nil
	}

	return "", fmt.Errorf("unsupported image type: %s", img.Type)
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

	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
			Details: "KlingAI video models do not support seed for deterministic generation.",
		})
	}

	if opts.FPS != nil {
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
	opts.PollIntervalMs = 5000   // 5 seconds
	opts.PollTimeoutMs = 600000  // 10 minutes

	if provOpts.PollIntervalMs != nil {
		opts.PollIntervalMs = *provOpts.PollIntervalMs
	}

	if provOpts.PollTimeoutMs != nil {
		opts.PollTimeoutMs = *provOpts.PollTimeoutMs
	}

	return opts
}

// pollForCompletion polls the task status until completion or timeout.
// Returns the final status response, the HTTP headers from the last poll, and any error.
// Distinguishes between task failure (KLINGAI_VIDEO_GENERATION_FAILED) and timeout errors.
func (m *VideoModel) pollForCompletion(ctx context.Context, authToken, endpoint, taskID string, opts polling.PollOptions) (*taskStatusResponse, map[string]string, error) {
	statusURL := fmt.Sprintf("%s/%s", endpoint, taskID)

	// taskFailedErr captures the error when the KlingAI task explicitly fails
	// (task_status == "failed"), so we can distinguish it from a timeout.
	var taskFailedErr error
	var lastHeaders map[string]string

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		// Make status request with per-request auth header (avoids mutating shared client state)
		resp, err := m.prov.client.Do(ctx, internalhttp.Request{
			Method:  "GET",
			Path:    statusURL,
			Headers: map[string]string{"Authorization": "Bearer " + authToken},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check status: %w", err)
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("status check returned %d: %s", resp.StatusCode, string(resp.Body))
		}

		// Capture headers from every poll; the last successful one is used in the response.
		lastHeaders = convertHTTPHeaders(resp.Headers)

		var statusResp taskStatusResponse
		if err := json.Unmarshal(resp.Body, &statusResp); err != nil {
			return nil, fmt.Errorf("failed to parse status response: %w", err)
		}

		if statusResp.Code != 0 {
			taskFailedErr = NewVideoGenerationFailedError(statusResp.Message)
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: statusResp.Message}, nil
		}

		if statusResp.Data == nil {
			taskFailedErr = NewVideoGenerationError("no data in status response")
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: "no data in status response"}, nil
		}

		switch statusResp.Data.TaskStatus {
		case "succeed":
			return &polling.JobResult{
				Status:   polling.JobStatusCompleted,
				Metadata: map[string]interface{}{"response": &statusResp},
			}, nil
		case "failed":
			errMsg := statusResp.Data.TaskStatusMsg
			if errMsg == "" {
				errMsg = "Unknown error"
			}
			taskFailedErr = NewVideoGenerationFailedError(errMsg)
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: errMsg}, nil
		case "submitted", "processing":
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		default:
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		}
	}

	result, pollErr := polling.PollForCompletion(ctx, checker, opts)

	// Task explicitly failed — surface KLINGAI_VIDEO_GENERATION_FAILED, not a timeout.
	if taskFailedErr != nil {
		return nil, lastHeaders, taskFailedErr
	}

	if pollErr != nil {
		return nil, lastHeaders, NewTimeoutError(fmt.Sprintf("%dms", opts.PollTimeoutMs))
	}

	// Extract the status response from polling metadata.
	statusResp, ok := result.Metadata["response"].(*taskStatusResponse)
	if !ok {
		return nil, lastHeaders, NewVideoGenerationError("failed to extract status response from polling result")
	}

	return statusResp, lastHeaders, nil
}

// convertResponse converts the KlingAI response to SDK format.
// startTime is captured at the beginning of DoGenerate (matches TS currentDate behavior).
// responseHeaders are the HTTP headers from the final poll response.
func (m *VideoModel) convertResponse(resp *taskStatusResponse, taskID string, warnings []types.Warning, startTime time.Time, responseHeaders map[string]string) *provider.VideoModelV3Response {
	videos := []provider.VideoModelV3VideoData{}
	videoMetadata := []map[string]interface{}{}

	if resp.Data != nil && resp.Data.TaskResult != nil {
		for _, video := range resp.Data.TaskResult.Videos {
			if video.URL != "" {
				videos = append(videos, provider.VideoModelV3VideoData{
					Type:      "url",
					URL:       video.URL,
					MediaType: "video/mp4",
				})

				metadata := map[string]interface{}{
					"id":  video.ID,
					"url": video.URL,
				}
				if video.WatermarkURL != "" {
					metadata["watermarkUrl"] = video.WatermarkURL
				}
				if video.Duration != "" {
					metadata["duration"] = video.Duration
				}
				videoMetadata = append(videoMetadata, metadata)
			}
		}
	}

	if responseHeaders == nil {
		responseHeaders = map[string]string{}
	}

	return &provider.VideoModelV3Response{
		Videos:   videos,
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			"klingai": map[string]interface{}{
				"taskId": taskID,
				"videos": videoMetadata,
			},
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: startTime,
			ModelID:   m.modelID,
			Headers:   responseHeaders,
		},
	}
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
