package klingai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	return "klingai"
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

	// Submit generation request
	endpoint := m.getEndpoint()
	submitResp, err := m.prov.client.Post(ctx, endpoint, body)
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

	// Poll for completion
	statusResp, err := m.pollForCompletion(ctx, authToken, endpoint, taskID, pollOpts)
	if err != nil {
		return nil, err
	}

	// Convert response to SDK format
	return m.convertResponse(statusResp, taskID, warnings), nil
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
		body["duration"] = fmt.Sprintf("%.0f", *opts.Duration)
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

	// Image is not supported for T2V
	if opts.Image != nil {
		warnings = append(warnings, types.Warning{
			Type: "unsupported",
			Message: "KlingAI text-to-video does not support image input. " +
				"Use an image-to-video model instead.",
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
		body["duration"] = fmt.Sprintf("%.0f", *opts.Duration)
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

	// AspectRatio is not supported for I2V (determined by input image)
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type: "unsupported",
			Message: "KlingAI image-to-video does not support aspectRatio. " +
				"The output dimensions are determined by the input image.",
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
		return nil, nil, NewInvalidOptionsError("videoUrl is required for motion control")
	}
	if provOpts.CharacterOrientation == nil || *provOpts.CharacterOrientation == "" {
		return nil, nil, NewInvalidOptionsError("characterOrientation is required for motion control")
	}
	if provOpts.Mode == nil || *provOpts.Mode == "" {
		return nil, nil, NewInvalidOptionsError("mode is required for motion control")
	}

	body := map[string]interface{}{
		"video_url":              *provOpts.VideoUrl,
		"character_orientation":  *provOpts.CharacterOrientation,
		"mode":                   *provOpts.Mode,
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

	// AspectRatio and duration not supported for motion control
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type: "unsupported",
			Message: "KlingAI Motion Control does not support aspectRatio. " +
				"The output dimensions are determined by the reference image/video.",
		})
	}

	if opts.Duration != nil {
		warnings = append(warnings, types.Warning{
			Type: "unsupported",
			Message: "KlingAI Motion Control does not support custom duration. " +
				"The output duration matches the reference video duration.",
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
			Message: "KlingAI video models do not support the resolution option.",
		})
	}

	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Message: "KlingAI video models do not support seed for deterministic generation.",
		})
	}

	if opts.FPS != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Message: "KlingAI video models do not support custom FPS.",
		})
	}

	if opts.N > 1 {
		warnings = append(warnings, types.Warning{
			Type: "unsupported",
			Message: "KlingAI video models do not support generating multiple videos per call. " +
				"Only 1 video will be generated.",
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

// pollForCompletion polls the task status until completion or timeout
func (m *VideoModel) pollForCompletion(ctx context.Context, authToken, endpoint, taskID string, opts polling.PollOptions) (*taskStatusResponse, error) {
	statusURL := fmt.Sprintf("%s/%s", endpoint, taskID)

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		// Make status request with auth header
		m.prov.client.SetHeader("Authorization", "Bearer "+authToken)
		resp, err := m.prov.client.Get(ctx, statusURL)
		if err != nil {
			return nil, fmt.Errorf("failed to check status: %w", err)
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("status check returned %d: %s", resp.StatusCode, string(resp.Body))
		}

		var statusResp taskStatusResponse
		if err := json.Unmarshal(resp.Body, &statusResp); err != nil {
			return nil, fmt.Errorf("failed to parse status response: %w", err)
		}

		if statusResp.Code != 0 {
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  statusResp.Message,
			}, nil
		}

		if statusResp.Data == nil {
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  "no data in status response",
			}, nil
		}

		// Map KlingAI status to polling status
		switch statusResp.Data.TaskStatus {
		case "succeed":
			return &polling.JobResult{
				Status:   polling.JobStatusCompleted,
				Metadata: map[string]interface{}{"response": statusResp},
			}, nil
		case "failed":
			errMsg := statusResp.Data.TaskStatusMsg
			if errMsg == "" {
				errMsg = "unknown error"
			}
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  errMsg,
			}, nil
		case "submitted", "processing":
			return &polling.JobResult{
				Status: polling.JobStatusProcessing,
			}, nil
		default:
			return &polling.JobResult{
				Status: polling.JobStatusProcessing,
			}, nil
		}
	}

	result, err := polling.PollForCompletion(ctx, checker, opts)
	if err != nil {
		return nil, NewTimeoutError(fmt.Sprintf("%dms", opts.PollTimeoutMs))
	}

	// Extract response from metadata
	statusResp, ok := result.Metadata["response"].(*taskStatusResponse)
	if !ok {
		return nil, NewVideoGenerationError("failed to extract status response from polling result")
	}

	return statusResp, nil
}

// convertResponse converts the KlingAI response to SDK format
func (m *VideoModel) convertResponse(resp *taskStatusResponse, taskID string, warnings []types.Warning) *provider.VideoModelV3Response {
	videos := []provider.VideoModelV3VideoData{}
	videoMetadata := []map[string]interface{}{}

	if resp.Data != nil && resp.Data.TaskResult != nil && len(resp.Data.TaskResult.Videos) > 0 {
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
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}
}

// extractProviderOptions extracts KlingAI provider options from the generic options map
func extractProviderOptions(opts map[string]interface{}) (*ProviderOptions, error) {
	if opts == nil {
		return &ProviderOptions{}, nil
	}

	klingaiOpts, ok := opts["klingai"]
	if !ok {
		return &ProviderOptions{}, nil
	}

	// Convert to JSON and back to get proper typing
	jsonData, err := json.Marshal(klingaiOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal provider options: %w", err)
	}

	var provOpts ProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal provider options: %w", err)
	}

	return &provOpts, nil
}
