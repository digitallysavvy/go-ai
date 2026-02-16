package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// VideoModel implements the provider.VideoModelV3 interface for AI Gateway
type VideoModel struct {
	provider *Provider
	modelID  string
}

// NewVideoModel creates a new AI Gateway video model
func NewVideoModel(provider *Provider, modelID string) *VideoModel {
	return &VideoModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "gateway"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns the maximum videos per call
// Gateway sets a very large number to prevent client-side splitting
func (m *VideoModel) MaxVideosPerCall() *int {
	maxVideos := 9007199254740991 // JavaScript Number.MAX_SAFE_INTEGER
	return &maxVideos
}

// DoGenerate generates videos based on the given options
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Build request body
	body := map[string]interface{}{}

	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	if opts.N > 0 {
		body["n"] = opts.N
	}

	if opts.AspectRatio != "" {
		body["aspectRatio"] = opts.AspectRatio
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

	// Handle image file for image-to-video generation
	if opts.Image != nil {
		encodedImage, err := m.encodeVideoFile(opts.Image)
		if err != nil {
			return nil, fmt.Errorf("failed to encode image file: %w", err)
		}
		body["image"] = encodedImage
	}

	// Add provider-specific options
	if len(opts.ProviderOptions) > 0 {
		body["providerOptions"] = opts.ProviderOptions
	}

	// Add headers
	headers := m.getModelConfigHeaders()

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders()
	AddO11yHeaders(headers, o11y)

	// Add custom headers from options
	for k, v := range opts.Headers {
		headers[k] = v
	}

	// Make API request
	var responseData struct {
		Videos           []videoData                `json:"videos"`
		Warnings         []warningData              `json:"warnings,omitempty"`
		ProviderMetadata map[string]interface{}     `json:"providerMetadata,omitempty"`
	}

	err := m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/video-model",
		Body:    body,
		Headers: headers,
	}, &responseData)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response to VideoModelV3Response
	result := &provider.VideoModelV3Response{
		Videos:           make([]provider.VideoModelV3VideoData, 0, len(responseData.Videos)),
		Warnings:         make([]types.Warning, 0, len(responseData.Warnings)),
		ProviderMetadata: responseData.ProviderMetadata,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   headers,
		},
	}

	// Convert videos
	for _, video := range responseData.Videos {
		result.Videos = append(result.Videos, provider.VideoModelV3VideoData{
			Type:      video.Type,
			URL:       video.URL,
			Data:      video.Data,
			MediaType: video.MediaType,
		})
	}

	// Convert warnings
	for _, warning := range responseData.Warnings {
		// Construct message from warning fields
		message := warning.Message
		if message == "" && warning.Feature != "" {
			message = fmt.Sprintf("%s: %s", warning.Feature, warning.Details)
		} else if warning.Feature != "" {
			message = fmt.Sprintf("%s (%s: %s)", message, warning.Feature, warning.Details)
		}

		result.Warnings = append(result.Warnings, types.Warning{
			Type:    warning.Type,
			Message: message,
		})
	}

	return result, nil
}

// videoData represents video data in the API response
type videoData struct {
	Type      string `json:"type"`      // "url" or "base64"
	URL       string `json:"url,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"mediaType"`
}

// warningData represents a warning in the API response
type warningData struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Feature string `json:"feature,omitempty"`
	Details string `json:"details,omitempty"`
}

// encodeVideoFile encodes a video file for transmission
func (m *VideoModel) encodeVideoFile(file *provider.VideoModelV3File) (interface{}, error) {
	if file.Type == "url" {
		return file.URL, nil
	}

	if file.Type == "file" && len(file.Data) > 0 {
		// Encode binary data as base64
		mediaType := file.MediaType
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		encoded := base64.StdEncoding.EncodeToString(file.Data)
		return map[string]interface{}{
			"type":      "file",
			"data":      encoded,
			"mediaType": mediaType,
		}, nil
	}

	return nil, fmt.Errorf("invalid video file: must have either URL or binary data")
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *VideoModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-video-model-specification-version": "3",
		"ai-video-model-id":                    m.modelID,
	}
}

// handleError converts errors to appropriate provider errors
func (m *VideoModel) handleError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's a timeout error and convert to GatewayTimeoutError
	if gatewayerrors.IsTimeoutError(err) {
		return gatewayerrors.ConvertToGatewayTimeoutError(err, "gateway")
	}

	// Check if it's already a provider error
	if providererrors.IsProviderError(err) {
		return err
	}

	// Return as ProviderError
	return providererrors.NewProviderError("gateway", 0, "", err.Error(), err)
}
