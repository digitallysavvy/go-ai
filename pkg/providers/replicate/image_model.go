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
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "replicate"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

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
		return nil, fmt.Errorf("Replicate API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var prediction replicateImagePrediction
	if err := json.Unmarshal(resp.Body, &prediction); err != nil {
		return nil, fmt.Errorf("failed to decode prediction response: %w", err)
	}

	// With Prefer: wait, the response should already be complete
	// But we'll check status and poll if needed as fallback
	if prediction.Status != "succeeded" {
		prediction, err = m.pollImagePrediction(ctx, prediction.ID)
		if err != nil {
			return nil, err
		}
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

func (m *ImageModel) pollImagePrediction(ctx context.Context, predictionID string) (replicateImagePrediction, error) {
	maxAttempts := 60
	pollInterval := 2 * time.Second

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

		time.Sleep(pollInterval)
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
