package bytedance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// default polling configuration matching TS SDK defaults
const (
	defaultPollIntervalMs = 3000   // 3 seconds
	defaultPollTimeoutMs  = 300000 // 5 minutes
)

// VideoModel implements the provider.VideoModelV3 interface for ByteDance
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
	return "v3"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "bytedance.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (ByteDance generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	return nil
}

// DoGenerate performs video generation with async polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	warnings := []types.Warning{}

	// Extract provider options
	provOpts, err := extractProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	// Warn about unsupported standard options
	if opts.FPS != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Message: "ByteDance video models do not support custom FPS. Frame rate is fixed at 24 fps.",
		})
	}

	if opts.N > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Message: "ByteDance video models do not support generating multiple videos per call. Only 1 video will be generated.",
		})
	}

	// Build request body
	body, err := m.buildRequestBody(opts, provOpts)
	if err != nil {
		return nil, err
	}

	// Submit the generation task
	submitResp, err := m.prov.client.Post(ctx, "/contents/generations/tasks", body)
	if err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to submit request: %v", err))
	}

	if submitResp.StatusCode >= 400 {
		return nil, m.parseAPIError(submitResp.Body, submitResp.StatusCode)
	}

	// Parse task creation response
	var createResp taskCreateResponse
	if err := json.Unmarshal(submitResp.Body, &createResp); err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to parse creation response: %v", err))
	}

	taskID := createResp.ID
	if taskID == "" {
		return nil, NewVideoGenerationError("No task ID returned from API")
	}

	// Determine polling intervals
	pollIntervalMs := defaultPollIntervalMs
	pollTimeoutMs := defaultPollTimeoutMs

	if provOpts.PollIntervalMs != nil {
		pollIntervalMs = *provOpts.PollIntervalMs
	}
	if provOpts.PollTimeoutMs != nil {
		pollTimeoutMs = *provOpts.PollTimeoutMs
	}

	// Poll for completion
	statusResp, err := m.pollForCompletion(ctx, taskID, pollIntervalMs, pollTimeoutMs)
	if err != nil {
		return nil, err
	}

	// Extract video URL
	videoURL := ""
	if statusResp.Content != nil {
		videoURL = statusResp.Content.VideoURL
	}
	if videoURL == "" {
		return nil, NewVideoGenerationError("No video URL in response")
	}

	// Build provider metadata
	providerMeta := map[string]interface{}{
		"taskId": taskID,
	}
	if statusResp.Usage != nil {
		providerMeta["usage"] = statusResp.Usage
	}

	return &provider.VideoModelV3Response{
		Videos: []provider.VideoModelV3VideoData{
			{
				Type:      "url",
				URL:       videoURL,
				MediaType: "video/mp4",
			},
		},
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			"bytedance": providerMeta,
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}, nil
}

// buildRequestBody builds the API request body from call options
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions, provOpts *ProviderOptions) (map[string]interface{}, error) {
	content := []map[string]interface{}{}

	// Add text prompt
	if opts.Prompt != "" {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": opts.Prompt,
		})
	}

	// Add start frame image
	if opts.Image != nil {
		imageURL, err := encodeImage(opts.Image)
		if err != nil {
			return nil, fmt.Errorf("failed to encode image: %w", err)
		}
		content = append(content, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": imageURL,
			},
		})
	}

	// Add last frame image if provided
	if provOpts.LastFrameImage != nil && *provOpts.LastFrameImage != "" {
		content = append(content, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": *provOpts.LastFrameImage,
			},
			"role": "last_frame",
		})
	}

	// Add reference images if provided
	for _, refURL := range provOpts.ReferenceImages {
		content = append(content, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": refURL,
			},
			"role": "reference_image",
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

	if opts.Duration != nil {
		body["duration"] = *opts.Duration
	}

	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	if opts.Resolution != "" {
		body["resolution"] = mapResolution(opts.Resolution)
	}

	// Provider-specific options
	if provOpts.Watermark != nil {
		body["watermark"] = *provOpts.Watermark
	}

	if provOpts.GenerateAudio != nil {
		body["generate_audio"] = *provOpts.GenerateAudio
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

// pollForCompletion polls the ByteDance task status endpoint until the task succeeds or fails
func (m *VideoModel) pollForCompletion(ctx context.Context, taskID string, pollIntervalMs, pollTimeoutMs int) (*taskStatusResponse, error) {
	statusPath := fmt.Sprintf("/contents/generations/tasks/%s", taskID)

	var finalResponse *taskStatusResponse
	var jobFailureErr error

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		resp, err := m.prov.client.Get(ctx, statusPath)
		if err != nil {
			return nil, fmt.Errorf("failed to check status: %w", err)
		}

		if resp.StatusCode >= 400 {
			return nil, m.parseAPIError(resp.Body, resp.StatusCode)
		}

		var statusResp taskStatusResponse
		if err := json.Unmarshal(resp.Body, &statusResp); err != nil {
			return nil, fmt.Errorf("failed to parse status response: %w", err)
		}

		switch statusResp.Status {
		case "succeeded":
			finalResponse = &statusResp
			return &polling.JobResult{
				Status:   polling.JobStatusCompleted,
				Metadata: map[string]interface{}{"response": &statusResp},
			}, nil

		case "failed":
			failMsg := fmt.Sprintf("Video generation failed: %s", mustMarshal(statusResp))
			jobFailureErr = NewVideoGenerationError(failMsg)
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  failMsg,
			}, nil

		default:
			// "processing", "pending", or any other status
			return &polling.JobResult{
				Status: polling.JobStatusProcessing,
			}, nil
		}
	}

	pollOpts := polling.PollOptions{
		PollIntervalMs: pollIntervalMs,
		PollTimeoutMs:  pollTimeoutMs,
	}

	_, err := polling.PollForCompletion(ctx, checker, pollOpts)
	if err != nil {
		// Context cancellation takes priority
		if ctx.Err() != nil {
			return nil, fmt.Errorf("video generation aborted: %w", ctx.Err())
		}
		// Job explicitly failed (status = "failed")
		if jobFailureErr != nil {
			return nil, jobFailureErr
		}
		// Otherwise it was a polling timeout
		return nil, NewTimeoutError(fmt.Sprintf("%dms", pollTimeoutMs))
	}

	if finalResponse == nil {
		return nil, NewVideoGenerationError("failed to extract status response from polling result")
	}

	return finalResponse, nil
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

// API response types

// taskCreateResponse is the response from task creation
type taskCreateResponse struct {
	ID string `json:"id"`
}

// taskStatusResponse is the response from task status polling
type taskStatusResponse struct {
	ID      string              `json:"id"`
	Model   string              `json:"model"`
	Status  string              `json:"status"`
	Content *taskContentField   `json:"content"`
	Usage   *taskUsageField     `json:"usage"`
}

// taskContentField holds the video URL in the status response
type taskContentField struct {
	VideoURL string `json:"video_url"`
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
