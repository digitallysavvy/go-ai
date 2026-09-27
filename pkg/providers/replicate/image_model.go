package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Replicate
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Replicate image generation model
func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "replicate"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// ReplicateImagePollOptions holds the polling-related provider options
// forwarded via ImageGenerateOptions.ProviderOptions["replicate"]. It mirrors
// the TS SDK's replicateImageModelOptionsSchema pollIntervalMillis /
// maxPollAttempts fields.
type ReplicateImagePollOptions struct {
	// PollIntervalMillis is the delay between poll attempts when a prediction
	// exceeds the synchronous "Prefer: wait" duration. Defaults to 500ms.
	PollIntervalMillis *int
	// MaxPollAttempts is the maximum number of poll attempts before giving up.
	// Defaults to 240.
	MaxPollAttempts *int
}

const (
	defaultPollIntervalMillis = 500
	defaultMaxPollAttempts    = 240
)

// extractImagePollOptions reads pollIntervalMillis/maxPollAttempts from
// opts.ProviderOptions["replicate"], falling back to the TS SDK defaults.
func extractImagePollOptions(opts *provider.ImageGenerateOptions) ReplicateImagePollOptions {
	result := ReplicateImagePollOptions{}
	if opts == nil || opts.ProviderOptions == nil {
		return result
	}
	raw, ok := opts.ProviderOptions["replicate"].(map[string]interface{})
	if !ok {
		return result
	}
	if v, ok := numberOption(raw["pollIntervalMillis"]); ok {
		result.PollIntervalMillis = &v
	}
	if v, ok := numberOption(raw["maxPollAttempts"]); ok {
		result.MaxPollAttempts = &v
	}
	return result
}

func numberOption(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)
	pollOpts := extractImagePollOptions(opts)

	// Create prediction with Prefer: wait header
	// This tells Replicate to wait for completion instead of returning immediately
	req := internalhttp.Request{
		Method: "POST",
		Path:   "/predictions",
		Body:   reqBody,
		Headers: map[string]string{
			"Prefer": "wait",
		},
	}

	resp, err := m.provider.client.Do(ctx, req)
	if err != nil {
		return nil, providererrors.NewProviderError("replicate", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return nil, fmt.Errorf("replicate API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var prediction replicateImagePrediction
	if err := json.Unmarshal(resp.Body, &prediction); err != nil {
		return nil, fmt.Errorf("failed to decode prediction response: %w", err)
	}

	// With Prefer: wait, the response should already be complete
	// But we'll check status and poll if needed as fallback
	if prediction.Status != "succeeded" {
		prediction, err = m.pollImagePrediction(ctx, prediction.ID, pollOpts)
		if err != nil {
			return nil, err
		}
	}

	if prediction.Output == nil {
		return nil, providererrors.NewInvalidResponseDataError(prediction, "Replicate image generation completed without output.")
	}

	return m.convertResponse(ctx, prediction)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	input := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	if opts.N != nil && *opts.N > 1 {
		input["num_outputs"] = *opts.N
	}

	if opts.Size != "" {
		var width, height int
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			input["width"] = width
			input["height"] = height
		}
	}

	return map[string]interface{}{
		"version": m.modelID,
		"input":   input,
	}
}

func (m *ImageModel) pollImagePrediction(ctx context.Context, predictionID string, pollOpts ReplicateImagePollOptions) (replicateImagePrediction, error) {
	maxAttempts := defaultMaxPollAttempts
	if pollOpts.MaxPollAttempts != nil {
		maxAttempts = *pollOpts.MaxPollAttempts
	}
	pollIntervalMillis := defaultPollIntervalMillis
	if pollOpts.PollIntervalMillis != nil {
		pollIntervalMillis = *pollOpts.PollIntervalMillis
	}
	pollInterval := time.Duration(pollIntervalMillis) * time.Millisecond

	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return replicateImagePrediction{}, ctx.Err()
		default:
		}

		resp, err := m.provider.client.Get(ctx, "/predictions/"+predictionID)
		if err != nil {
			return replicateImagePrediction{}, err
		}

		var prediction replicateImagePrediction
		if err := json.Unmarshal(resp.Body, &prediction); err != nil {
			return replicateImagePrediction{}, fmt.Errorf("failed to decode prediction: %w", err)
		}

		if prediction.Status == "succeeded" {
			return prediction, nil
		}

		if prediction.Status == "failed" || prediction.Status == "canceled" {
			return replicateImagePrediction{}, fmt.Errorf("prediction %s: %s", prediction.Status, prediction.Error)
		}

		if i < maxAttempts-1 {
			time.Sleep(pollInterval)
		}
	}

	return replicateImagePrediction{}, fmt.Errorf("prediction timed out after %d attempts", maxAttempts)
}

func (m *ImageModel) convertResponse(ctx context.Context, prediction replicateImagePrediction) (*types.ImageResult, error) {
	var imageURL string

	// Output is typically an array of image URLs
	switch v := prediction.Output.(type) {
	case []interface{}:
		if len(v) > 0 {
			if urlStr, ok := v[0].(string); ok {
				imageURL = urlStr
			}
		}
	case string:
		imageURL = v
	}

	if imageURL == "" {
		return nil, fmt.Errorf("no image URL in prediction output")
	}

	// Download image from URL
	imageData, err := m.downloadImage(ctx, imageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}

	return &types.ImageResult{
		Image:    imageData,
		MimeType: "image/png",
		URL:      imageURL,
		Usage:    types.ImageUsage{},
	}, nil
}

func (m *ImageModel) downloadImage(ctx context.Context, url string) ([]byte, error) {
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 30 * time.Second
	return fileutil.Download(ctx, url, opts)
}

type replicateImagePrediction struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output interface{} `json:"output"`
	Error  string      `json:"error"`
}
