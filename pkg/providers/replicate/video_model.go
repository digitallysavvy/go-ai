package replicate

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
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for Replicate
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Replicate video generation model
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
	return "replicate"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (Replicate generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	return nil // Default to 1
}

// DoGenerate performs video generation with polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Build prediction request
	predReq := m.buildPredictionRequest(opts)

	// Create prediction
	resp, err := m.prov.client.Post(ctx, "/predictions", predReq)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "failed to create prediction", err)
	}

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return nil, providererrors.NewVideoGenerationError("replicate", m.modelID,
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(resp.Body)), nil)
	}

	// Parse prediction response
	var prediction replicateVideoPrediction
	if err := json.Unmarshal(resp.Body, &prediction); err != nil {
		return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "failed to parse prediction response", err)
	}

	// Check if prediction is already complete
	if prediction.Status == "succeeded" && len(prediction.Output) > 0 {
		return m.convertResponse(ctx, &prediction)
	}

	// Poll for completion
	pollOpts := m.getPollOptions(opts.ProviderOptions)
	result, err := m.pollForCompletion(ctx, prediction.ID, pollOpts)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(ctx, result)
}

// buildPredictionRequest builds the Replicate prediction request
func (m *VideoModel) buildPredictionRequest(opts *provider.VideoModelV3CallOptions) map[string]interface{} {
	input := make(map[string]interface{})

	// Add prompt
	if opts.Prompt != "" {
		input["prompt"] = opts.Prompt
	}

	// Add image if provided (for image-to-video)
	if opts.Image != nil {
		if opts.Image.Type == "url" {
			input["image"] = opts.Image.URL
		}
		// Note: For binary images, we'd need to upload to a URL first
	}

	// Add video parameters based on provider options
	if opts.AspectRatio != "" {
		input["aspect_ratio"] = opts.AspectRatio
	}

	if opts.Duration != nil {
		input["duration"] = *opts.Duration
	}

	if opts.FPS != nil {
		input["fps"] = *opts.FPS
	}

	if opts.Seed != nil {
		input["seed"] = *opts.Seed
	}

	// Add provider-specific options
	if opts.ProviderOptions != nil {
		if repOpts, ok := opts.ProviderOptions["replicate"].(map[string]interface{}); ok {
			for k, v := range repOpts {
				// Skip polling-related options
				if k != "pollIntervalMs" && k != "pollTimeoutMs" {
					input[k] = v
				}
			}
		}
	}

	return map[string]interface{}{
		"version": m.modelID,
		"input":   input,
	}
}

// getPollOptions extracts polling options from provider options
func (m *VideoModel) getPollOptions(providerOpts map[string]interface{}) polling.PollOptions {
	opts := polling.DefaultPollOptions()

	if providerOpts != nil {
		if repOpts, ok := providerOpts["replicate"].(map[string]interface{}); ok {
			if interval, ok := repOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := repOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

// pollForCompletion polls for prediction completion
func (m *VideoModel) pollForCompletion(ctx context.Context, predictionID string, opts polling.PollOptions) (*replicateVideoPrediction, error) {
	checker := func(ctx context.Context) (*polling.JobResult, error) {
		// Get prediction status
		path := fmt.Sprintf("/predictions/%s", predictionID)
		resp, err := m.prov.client.Get(ctx, path)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("status check failed: status %d", resp.StatusCode)
		}

		var prediction replicateVideoPrediction
		if err := json.Unmarshal(resp.Body, &prediction); err != nil {
			return nil, fmt.Errorf("failed to parse prediction: %w", err)
		}

		// Convert Replicate status to JobStatus
		switch prediction.Status {
		case "starting", "processing":
			return &polling.JobResult{
				Status:   polling.JobStatusProcessing,
				Progress: 50, // Replicate doesn't provide progress
			}, nil

		case "succeeded":
			return &polling.JobResult{
				Status: polling.JobStatusCompleted,
				Metadata: map[string]interface{}{
					"prediction": prediction,
				},
			}, nil

		case "failed":
			errorMsg := "unknown error"
			if prediction.Error != "" {
				errorMsg = prediction.Error
			}
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  errorMsg,
			}, nil

		case "canceled":
			return &polling.JobResult{
				Status: polling.JobStatusCancelled,
			}, nil

		default:
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  fmt.Sprintf("unknown status: %s", prediction.Status),
			}, nil
		}
	}

	result, err := polling.PollForCompletion(ctx, checker, opts)
	if err != nil {
		return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "polling failed", err)
	}

	// Extract prediction from metadata
	if result.Metadata != nil {
		if pred, ok := result.Metadata["prediction"].(replicateVideoPrediction); ok {
			return &pred, nil
		}
	}

	return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "no prediction in completed job", nil)
}

// convertResponse converts Replicate prediction to VideoModelV3Response
func (m *VideoModel) convertResponse(ctx context.Context, prediction *replicateVideoPrediction) (*provider.VideoModelV3Response, error) {
	if len(prediction.Output) == 0 {
		return nil, providererrors.NewNoVideoGeneratedError()
	}

	// Replicate typically returns a URL or array of URLs
	var videoURL string
	switch v := prediction.Output[0].(type) {
	case string:
		videoURL = v
	default:
		return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "unexpected output format", nil)
	}

	// Download the video
	videoData, err := m.downloadVideo(ctx, videoURL)
	if err != nil {
		// If download fails, return URL anyway
		return &provider.VideoModelV3Response{
			Videos: []provider.VideoModelV3VideoData{
				{
					Type:      "url",
					URL:       videoURL,
					MediaType: "video/mp4",
				},
			},
			Warnings: []types.Warning{
				{
					Type:    "download_failed",
					Message: fmt.Sprintf("Failed to download video: %v", err),
				},
			},
			ProviderMetadata: map[string]interface{}{
				"predictionId": prediction.ID,
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
				URL:       videoURL,
				MediaType: mediaType,
			},
		},
		Warnings: []types.Warning{},
		ProviderMetadata: map[string]interface{}{
			"predictionId": prediction.ID,
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}, nil
}

// downloadVideo downloads video from a URL with size limits to prevent DoS
func (m *VideoModel) downloadVideo(ctx context.Context, url string) ([]byte, error) {
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 60 * time.Second
	return fileutil.Download(ctx, url, opts)
}

// Response types for Replicate API

type replicateVideoPrediction struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Output []interface{} `json:"output"`
	Error  string        `json:"error,omitempty"`
}
