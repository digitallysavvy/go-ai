package google

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// VideoModel implements the provider.VideoModelV3 interface for Google Generative AI
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Google Generative AI video generation model
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
	return "google"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (Google Generative AI generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	return nil // Default to 1
}

// DoGenerate performs video generation with polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts)

	// Submit generation request
	path := fmt.Sprintf("/v1beta/models/%s:generateVideo", m.modelID)

	// Add API key to query parameters
	queryParams := map[string]string{
		"key": m.prov.APIKey(),
	}

	// Build full URL with query parameters
	fullPath := path
	if len(queryParams) > 0 {
		fullPath += "?"
		first := true
		for k, v := range queryParams {
			if !first {
				fullPath += "&"
			}
			fullPath += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
	}

	submitResp, err := m.prov.client.Post(ctx, fullPath, reqBody)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "failed to submit request", err)
	}

	if submitResp.StatusCode != 200 && submitResp.StatusCode != 201 {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID,
			fmt.Sprintf("API returned status %d: %s", submitResp.StatusCode, string(submitResp.Body)), nil)
	}

	// Parse submission response
	var submitResult googleVideoOperation
	if err := json.Unmarshal(submitResp.Body, &submitResult); err != nil {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "failed to parse submit response", err)
	}

	// Google Generative AI uses long-running operations
	// Poll for completion
	if submitResult.Name == "" {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "no operation name in response", nil)
	}

	// Get polling options from provider options
	pollOpts := m.getPollOptions(opts.ProviderOptions)

	// Poll for completion
	result, err := m.pollForCompletion(ctx, submitResult.Name, pollOpts)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(ctx, result)
}

// buildRequestBody builds the API request body
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions) map[string]interface{} {
	body := make(map[string]interface{})

	// Add prompt (required for text-to-video)
	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	// Add image if provided (for image-to-video)
	if opts.Image != nil {
		imageData := make(map[string]interface{})
		if opts.Image.Type == "url" {
			// Google requires base64 or inline data, not URLs
			// We'll handle this by downloading and converting
			imageData["url"] = opts.Image.URL
		} else if opts.Image.Type == "file" && len(opts.Image.Data) > 0 {
			// Convert to base64
			imageData["bytesBase64Encoded"] = opts.Image.Data
			imageData["mimeType"] = opts.Image.MediaType
		}
		body["image"] = imageData
	}

	// Build generation config
	config := make(map[string]interface{})

	if opts.AspectRatio != "" {
		config["aspectRatio"] = opts.AspectRatio
	}

	if opts.Resolution != "" {
		// Map common resolutions to Google format
		resolutionMap := map[string]string{
			"1280x720":  "720p",
			"1920x1080": "1080p",
			"3840x2160": "4k",
		}
		if mapped, ok := resolutionMap[opts.Resolution]; ok {
			config["resolution"] = mapped
		} else {
			config["resolution"] = opts.Resolution
		}
	}

	if opts.Duration != nil {
		config["durationSeconds"] = *opts.Duration
	}

	if opts.Seed != nil {
		config["seed"] = *opts.Seed
	}

	// Add provider-specific options
	if opts.ProviderOptions != nil {
		if googleOpts, ok := opts.ProviderOptions["google"].(map[string]interface{}); ok {
			for k, v := range googleOpts {
				// Skip polling-related options
				if k != "pollIntervalMs" && k != "pollTimeoutMs" {
					config[k] = v
				}
			}
		}
	}

	if len(config) > 0 {
		body["generationConfig"] = config
	}

	return body
}

// getPollOptions extracts polling options from provider options
func (m *VideoModel) getPollOptions(providerOpts map[string]interface{}) polling.PollOptions {
	opts := polling.DefaultPollOptions()

	if providerOpts != nil {
		if googleOpts, ok := providerOpts["google"].(map[string]interface{}); ok {
			if interval, ok := googleOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := googleOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

// pollForCompletion polls Google's operation endpoint until completion
func (m *VideoModel) pollForCompletion(ctx context.Context, operationName string, opts polling.PollOptions) (*googleVideoOperationResult, error) {
	var result *googleVideoOperationResult

	statusChecker := func(ctx context.Context) (*polling.JobResult, error) {
		// Get operation status
		path := fmt.Sprintf("/v1beta/%s?key=%s", operationName, m.prov.APIKey())

		resp, err := m.prov.client.Get(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("failed to check operation status: %w", err)
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("operation status check returned %d: %s", resp.StatusCode, string(resp.Body))
		}

		var operation googleVideoOperation
		if err := json.Unmarshal(resp.Body, &operation); err != nil {
			return nil, fmt.Errorf("failed to parse operation response: %w", err)
		}

		// Check if operation is complete
		if operation.Done {
			// Check for errors
			if operation.Error != nil {
				return &polling.JobResult{
					Status: polling.JobStatusFailed,
					Error:  fmt.Sprintf("operation failed: %s (code: %d)", operation.Error.Message, operation.Error.Code),
				}, nil
			}

			// Operation successful, extract result
			if operation.Response != nil {
				result = operation.Response
				return &polling.JobResult{
					Status: polling.JobStatusCompleted,
				}, nil
			}

			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  "operation completed but no response data",
			}, nil
		}

		// Not done yet, continue polling
		return &polling.JobResult{
			Status: polling.JobStatusProcessing,
		}, nil
	}

	jobResult, err := polling.PollForCompletion(ctx, statusChecker, opts)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "polling failed", err)
	}

	if jobResult.Status != polling.JobStatusCompleted || result == nil {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "polling completed but no result", nil)
	}

	return result, nil
}

// convertResponse converts Google API response to VideoModelV3Response
func (m *VideoModel) convertResponse(ctx context.Context, result *googleVideoOperationResult) (*provider.VideoModelV3Response, error) {
	if len(result.GeneratedVideos) == 0 {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "no videos in response", nil)
	}

	videos := make([]provider.VideoModelV3VideoData, 0, len(result.GeneratedVideos))

	for _, gv := range result.GeneratedVideos {
		// Download video data
		videoData, mediaType, err := m.downloadVideo(ctx, gv.VideoURI)
		if err != nil {
			return nil, providererrors.NewVideoGenerationError("google", m.modelID,
				fmt.Sprintf("failed to download video: %v", err), err)
		}

		videos = append(videos, provider.VideoModelV3VideoData{
			Type:      "binary",
			Binary:    videoData,
			MediaType: mediaType,
		})
	}

	response := &provider.VideoModelV3Response{
		Videos: videos,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
		},
	}

	return response, nil
}

// downloadVideo downloads video from URL and detects media type with size limits to prevent DoS
func (m *VideoModel) downloadVideo(ctx context.Context, url string) ([]byte, string, error) {
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 5 * time.Minute // Videos can be large

	data, err := fileutil.Download(ctx, url, opts)
	if err != nil {
		return nil, "", err
	}

	// Detect media type from content
	mediaType := media.DetectVideoMediaType(data)
	if mediaType == "" {
		mediaType = "video/mp4" // Default assumption
	}

	return data, mediaType, nil
}

// Response types

// googleVideoOperation represents a long-running operation
type googleVideoOperation struct {
	Name     string                      `json:"name"`
	Done     bool                        `json:"done"`
	Error    *googleOperationError       `json:"error,omitempty"`
	Response *googleVideoOperationResult `json:"response,omitempty"`
	Metadata map[string]interface{}      `json:"metadata,omitempty"`
}

// googleOperationError represents an operation error
type googleOperationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// googleVideoOperationResult represents the successful operation result
type googleVideoOperationResult struct {
	GeneratedVideos []googleGeneratedVideo `json:"generatedVideos"`
}

// googleGeneratedVideo represents a single generated video
type googleGeneratedVideo struct {
	VideoURI string `json:"videoUri"`
	CRC32C   string `json:"crc32c,omitempty"`
}
