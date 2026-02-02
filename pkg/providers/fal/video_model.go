package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for Fal.ai
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Fal.ai video generation model
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
	return "fal"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (FAL generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	return nil // Default to 1
}

// DoGenerate performs video generation with polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts)

	// Submit generation request
	path := fmt.Sprintf("/%s", m.modelID)
	submitResp, err := m.prov.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "failed to submit request", err)
	}

	if submitResp.StatusCode != 200 {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID,
			fmt.Sprintf("API returned status %d: %s", submitResp.StatusCode, string(submitResp.Body)), nil)
	}

	// Parse submission response
	var submitResult falSubmitResponse
	if err := json.Unmarshal(submitResp.Body, &submitResult); err != nil {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "failed to parse submit response", err)
	}

	// If response has immediate video URL, return it
	if submitResult.Video != nil && submitResult.Video.URL != "" {
		return m.convertImmediateResponse(ctx, submitResult)
	}

	// Otherwise, poll for completion using request ID
	if submitResult.RequestID == "" {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "no request ID or video URL in response", nil)
	}

	// Get polling options from provider options
	pollOpts := m.getPollOptions(opts.ProviderOptions)

	// Poll for completion
	result, err := m.pollForCompletion(ctx, submitResult.RequestID, pollOpts)
	if err != nil {
		return nil, err
	}

	return m.convertPolledResponse(ctx, result)
}

// buildRequestBody builds the API request body
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions) map[string]interface{} {
	body := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	// Add image if provided (for image-to-video)
	if opts.Image != nil {
		if opts.Image.Type == "url" {
			body["image_url"] = opts.Image.URL
		} else if opts.Image.Type == "file" {
			// For FAL, we'd need to upload the image first or use base64
			// For now, we'll skip this - it would require additional API call
		}
	}

	// Add video parameters
	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	if opts.Resolution != "" {
		body["resolution"] = opts.Resolution
	}

	if opts.Duration != nil {
		body["duration"] = *opts.Duration
	}

	if opts.FPS != nil {
		body["fps"] = *opts.FPS
	}

	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// Add provider-specific options
	if opts.ProviderOptions != nil {
		if falOpts, ok := opts.ProviderOptions["fal"].(map[string]interface{}); ok {
			for k, v := range falOpts {
				// Skip polling-related options
				if k != "pollIntervalMs" && k != "pollTimeoutMs" {
					body[k] = v
				}
			}
		}
	}

	return body
}

// getPollOptions extracts polling options from provider options
func (m *VideoModel) getPollOptions(providerOpts map[string]interface{}) polling.PollOptions {
	opts := polling.DefaultPollOptions()

	if providerOpts != nil {
		if falOpts, ok := providerOpts["fal"].(map[string]interface{}); ok {
			if interval, ok := falOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := falOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

// pollForCompletion polls for video generation completion
func (m *VideoModel) pollForCompletion(ctx context.Context, requestID string, opts polling.PollOptions) (*falVideoResponse, error) {
	checker := func(ctx context.Context) (*polling.JobResult, error) {
		// Check status
		statusPath := fmt.Sprintf("/requests/%s/status", requestID)
		statusResp, err := m.prov.client.Get(ctx, statusPath)
		if err != nil {
			return nil, err
		}

		if statusResp.StatusCode != 200 {
			return nil, fmt.Errorf("status check failed: status %d", statusResp.StatusCode)
		}

		var status falStatusResponse
		if err := json.Unmarshal(statusResp.Body, &status); err != nil {
			return nil, fmt.Errorf("failed to parse status response: %w", err)
		}

		// Convert FAL status to JobStatus
		switch status.Status {
		case "IN_QUEUE", "IN_PROGRESS":
			return &polling.JobResult{
				Status:   polling.JobStatusProcessing,
				Progress: status.Progress,
			}, nil

		case "COMPLETED":
			// Return completed with the full response
			return &polling.JobResult{
				Status: polling.JobStatusCompleted,
				Metadata: map[string]interface{}{
					"response": status,
				},
			}, nil

		case "FAILED":
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  status.Error,
			}, nil

		default:
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  fmt.Sprintf("unknown status: %s", status.Status),
			}, nil
		}
	}

	result, err := polling.PollForCompletion(ctx, checker, opts)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "polling failed", err)
	}

	// Extract the response from metadata
	if result.Metadata != nil {
		if statusResp, ok := result.Metadata["response"].(falStatusResponse); ok {
			return &statusResp.Output, nil
		}
	}

	return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "no output in completed job", nil)
}

// convertImmediateResponse converts an immediate response to VideoModelV3Response
func (m *VideoModel) convertImmediateResponse(ctx context.Context, resp falSubmitResponse) (*provider.VideoModelV3Response, error) {
	if resp.Video == nil {
		return nil, providererrors.NewNoVideoGeneratedError()
	}

	return &provider.VideoModelV3Response{
		Videos: []provider.VideoModelV3VideoData{
			{
				Type:      "url",
				URL:       resp.Video.URL,
				MediaType: "video/mp4",
			},
		},
		Warnings: []types.Warning{},
		ProviderMetadata: map[string]interface{}{
			"requestId": resp.RequestID,
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}, nil
}

// convertPolledResponse converts a polled response to VideoModelV3Response
func (m *VideoModel) convertPolledResponse(ctx context.Context, resp *falVideoResponse) (*provider.VideoModelV3Response, error) {
	if resp.Video == nil || resp.Video.URL == "" {
		return nil, providererrors.NewNoVideoGeneratedError()
	}

	// Download the video
	videoData, err := m.downloadVideo(ctx, resp.Video.URL)
	if err != nil {
		// If download fails, return URL anyway
		return &provider.VideoModelV3Response{
			Videos: []provider.VideoModelV3VideoData{
				{
					Type:      "url",
					URL:       resp.Video.URL,
					MediaType: "video/mp4",
				},
			},
			Warnings: []types.Warning{
				{
					Type:    "download_failed",
					Message: fmt.Sprintf("Failed to download video: %v", err),
				},
			},
			Response: provider.VideoModelV3ResponseInfo{
				Timestamp: time.Now(),
				ModelID:   m.modelID,
				Headers:   map[string]string{},
			},
		}, nil
	}

	// Detect media type
	mediaType := media.DetectVideoMediaType(videoData)

	return &provider.VideoModelV3Response{
		Videos: []provider.VideoModelV3VideoData{
			{
				Type:      "binary",
				Binary:    videoData,
				URL:       resp.Video.URL,
				MediaType: mediaType,
			},
		},
		Warnings: []types.Warning{},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}, nil
}

// downloadVideo downloads video from a URL
func (m *VideoModel) downloadVideo(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to download video: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// Response types for FAL API

type falSubmitResponse struct {
	RequestID string          `json:"request_id,omitempty"`
	Video     *falVideoResult `json:"video,omitempty"`
}

type falStatusResponse struct {
	Status   string           `json:"status"`
	Progress int              `json:"progress,omitempty"`
	Output   falVideoResponse `json:"output,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type falVideoResponse struct {
	Video *falVideoResult `json:"video,omitempty"`
}

type falVideoResult struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
}
