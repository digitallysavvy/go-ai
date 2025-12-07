package bfl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Black Forest Labs
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new BFL image generation model
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
	return "bfl"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	// Create request
	endpoint := m.getEndpoint()
	resp, err := m.provider.client.Post(ctx, endpoint, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("bfl", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("BFL API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var createResp bflCreateResponse
	if err := json.Unmarshal(resp.Body, &createResp); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}

	// Poll for completion
	result, err := m.pollResult(ctx, createResp.ID)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(ctx, result)
}

func (m *ImageModel) getEndpoint() string {
	// Different FLUX models have different endpoints
	switch m.modelID {
	case "flux-pro":
		return "/flux-pro"
	case "flux-pro-1.1":
		return "/flux-pro-1.1"
	case "flux-dev":
		return "/flux-dev"
	case "flux-schnell":
		return "/flux-schnell"
	default:
		return "/flux-pro" // Default to pro
	}
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	// Parse size if provided
	if opts.Size != "" {
		var width, height int
		fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			reqBody["width"] = width
			reqBody["height"] = height
		}
	}

	return reqBody
}

func (m *ImageModel) pollResult(ctx context.Context, requestID string) (bflResult, error) {
	maxAttempts := 60
	pollInterval := 2 * time.Second

	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return bflResult{}, ctx.Err()
		default:
		}

		endpoint := fmt.Sprintf("/get_result?id=%s", requestID)
		resp, err := m.provider.client.Get(ctx, endpoint)
		if err != nil {
			return bflResult{}, err
		}

		var result bflResult
		if err := json.Unmarshal(resp.Body, &result); err != nil {
			return bflResult{}, fmt.Errorf("failed to decode result: %w", err)
		}

		if result.Status == "Ready" {
			return result, nil
		}

		if result.Status == "Error" || result.Status == "Request Moderated" {
			return bflResult{}, fmt.Errorf("request %s: %s", result.Status, result.ID)
		}

		// Status is "Pending" - continue polling
		time.Sleep(pollInterval)
	}

	return bflResult{}, fmt.Errorf("request timed out after %d attempts", maxAttempts)
}

func (m *ImageModel) convertResponse(ctx context.Context, result bflResult) (*types.ImageResult, error) {
	if result.Result.Sample == "" {
		return nil, fmt.Errorf("no image URL in result")
	}

	// Download image from URL
	imageData, err := m.downloadImage(ctx, result.Result.Sample)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}

	return &types.ImageResult{
		Image:    imageData,
		MimeType: "image/png",
		URL:      result.Result.Sample,
		Usage:    types.ImageUsage{},
	}, nil
}

func (m *ImageModel) downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type bflCreateResponse struct {
	ID string `json:"id"`
}

type bflResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // "Pending", "Ready", "Error", "Request Moderated"
	Result struct {
		Sample string `json:"sample"` // URL to the generated image
	} `json:"result"`
}
