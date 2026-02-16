package alibaba

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for Alibaba Wan models
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Alibaba video generation model
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
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
	return "alibaba.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// detectMode determines the model mode from model ID
func (m *VideoModel) detectMode() string {
	if strings.Contains(m.modelID, "-i2v") {
		return "i2v"
	}
	if strings.Contains(m.modelID, "-r2v") {
		return "r2v"
	}
	return "t2v"
}

// MaxVideosPerCall returns nil (Alibaba generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	return nil // Default to 1
}

// DoGenerate performs video generation with polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Build request body based on model type
	reqBody := m.buildRequestBody(opts)

	// Submit generation request to DashScope API
	path := "/api/v1/services/aigc/video-generation/video-synthesis"
	submitResp, err := m.prov.videoClient.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID, "failed to submit request", err)
	}

	if submitResp.StatusCode != 200 {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID,
			fmt.Sprintf("API returned status %d: %s", submitResp.StatusCode, string(submitResp.Body)), nil)
	}

	// Parse submission response
	var submitResult alibabaVideoSubmitResponse
	if err := json.Unmarshal(submitResp.Body, &submitResult); err != nil {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID, "failed to parse submit response", err)
	}

	if submitResult.Output.TaskID == "" {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID, "no task ID in response", nil)
	}

	// Poll for completion
	pollOpts := m.getPollOptions(opts.ProviderOptions)
	result, err := m.pollForCompletion(ctx, submitResult.Output.TaskID, pollOpts)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(ctx, result, submitResult.Output.TaskID)
}

// buildRequestBody builds the API request body based on model type
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions) map[string]interface{} {
	body := map[string]interface{}{
		"model": m.modelID,
		"input": map[string]interface{}{},
		"parameters": map[string]interface{}{},
	}

	input := body["input"].(map[string]interface{})
	params := body["parameters"].(map[string]interface{})

	// Determine model mode
	mode := m.detectMode()

	// Build input based on mode
	switch mode {
	case "t2v":
		// Text-to-video: just needs text prompt
		input["text"] = opts.Prompt

	case "i2v":
		// Image-to-video: needs image and optional text
		if opts.Image != nil {
			if opts.Image.Type == "url" {
				input["img_url"] = opts.Image.URL
			} else if opts.Image.Type == "file" {
				// Convert file data to base64
				base64Data := imageutil.EncodeToBase64(opts.Image.Data)
				input["img_url"] = base64Data
			}
		}
		if opts.Prompt != "" {
			input["text"] = opts.Prompt
		}

	case "r2v":
		// Reference-to-video: needs reference image and text prompt
		if opts.Image != nil {
			if opts.Image.Type == "url" {
				input["reference_image_url"] = opts.Image.URL
			}
		}
		input["text"] = opts.Prompt
	}

	// Add aspect ratio if specified
	if opts.AspectRatio != "" {
		params["aspect_ratio"] = opts.AspectRatio
	}

	// Add duration if specified (Alibaba uses seconds)
	if opts.Duration != nil {
		params["duration"] = *opts.Duration
	}

	// Add provider-specific options
	if opts.ProviderOptions != nil {
		if alibabaOpts, ok := opts.ProviderOptions["alibaba"].(map[string]interface{}); ok {
			for k, v := range alibabaOpts {
				// Skip polling options
				if k != "pollIntervalMs" && k != "pollTimeoutMs" {
					params[k] = v
				}
			}
		}
	}

	return body
}

// getPollOptions extracts polling options from provider options
func (m *VideoModel) getPollOptions(providerOpts map[string]interface{}) polling.PollOptions {
	opts := polling.DefaultPollOptions()
	opts.PollIntervalMs = 2000 // 2 seconds default for video
	opts.PollTimeoutMs = 300000 // 5 minutes default

	if providerOpts != nil {
		if alibabaOpts, ok := providerOpts["alibaba"].(map[string]interface{}); ok {
			if interval, ok := alibabaOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := alibabaOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

// pollForCompletion polls for video generation completion
func (m *VideoModel) pollForCompletion(ctx context.Context, taskID string, opts polling.PollOptions) (*alibabaVideoResult, error) {
	checker := func(ctx context.Context) (*polling.JobResult, error) {
		// Check status
		statusPath := fmt.Sprintf("/api/v1/tasks/%s", taskID)
		statusResp, err := m.prov.videoClient.Get(ctx, statusPath)
		if err != nil {
			return nil, err
		}

		if statusResp.StatusCode != 200 {
			return nil, fmt.Errorf("status check failed: status %d", statusResp.StatusCode)
		}

		var status alibabaVideoStatusResponse
		if err := json.Unmarshal(statusResp.Body, &status); err != nil {
			return nil, fmt.Errorf("failed to parse status response: %w", err)
		}

		// Convert Alibaba status to JobStatus
		switch status.Output.TaskStatus {
		case "PENDING", "RUNNING":
			return &polling.JobResult{
				Status:   polling.JobStatusProcessing,
				Progress: 0, // Alibaba doesn't provide progress percentage
			}, nil

		case "SUCCEEDED":
			return &polling.JobResult{
				Status: polling.JobStatusCompleted,
				Metadata: map[string]interface{}{
					"result": status.Output,
				},
			}, nil

		case "FAILED":
			errorMsg := "video generation failed"
			if status.Message != "" {
				errorMsg = status.Message
			}
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  errorMsg,
			}, nil

		default:
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  fmt.Sprintf("unknown status: %s", status.Output.TaskStatus),
			}, nil
		}
	}

	result, err := polling.PollForCompletion(ctx, checker, opts)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID, "polling failed", err)
	}

	// Extract the result from metadata
	if result.Metadata != nil {
		if videoResult, ok := result.Metadata["result"].(alibabaVideoResult); ok {
			return &videoResult, nil
		}
	}

	return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID, "no output in completed job", nil)
}

// convertResponse converts Alibaba video response to SDK format
func (m *VideoModel) convertResponse(ctx context.Context, result *alibabaVideoResult, taskID string) (*provider.VideoModelV3Response, error) {
	if result.VideoURL == "" {
		return nil, providererrors.NewNoVideoGeneratedError()
	}

	return &provider.VideoModelV3Response{
		Videos: []provider.VideoModelV3VideoData{
			{
				Type:      "url",
				URL:       result.VideoURL,
				MediaType: "video/mp4",
			},
		},
		Warnings: []types.Warning{},
		ProviderMetadata: map[string]interface{}{
			"taskId":     taskID,
			"taskStatus": result.TaskStatus,
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}, nil
}

// Response types for Alibaba video API

type alibabaVideoSubmitResponse struct {
	RequestID string `json:"request_id"`
	Output    struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
	} `json:"output"`
}

type alibabaVideoStatusResponse struct {
	RequestID string             `json:"request_id"`
	Output    alibabaVideoResult `json:"output"`
	Message   string             `json:"message,omitempty"`
}

type alibabaVideoResult struct {
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"` // PENDING, RUNNING, SUCCEEDED, FAILED
	VideoURL   string `json:"video_url,omitempty"`
}
