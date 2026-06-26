package gateway

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	return "v4"
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

// DoGenerate generates videos based on the given options via SSE stream
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Build request body
	body := map[string]interface{}{}

	body["prompt"] = opts.Prompt
	body["n"] = opts.N

	if opts.AspectRatio != "" {
		body["aspectRatio"] = opts.AspectRatio
	}

	if opts.Resolution != "" {
		body["resolution"] = opts.Resolution
	}

	if opts.Duration != nil && *opts.Duration != 0 {
		body["duration"] = *opts.Duration
	}

	if opts.FPS != nil && *opts.FPS != 0 {
		body["fps"] = *opts.FPS
	}

	if opts.Seed != nil && *opts.Seed != 0 {
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

	if opts.ProviderOptions != nil {
		body["providerOptions"] = opts.ProviderOptions
	}

	// Build headers
	headers := m.getModelConfigHeaders()

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)

	// Add custom headers from options
	for k, v := range opts.Headers {
		headers[k] = v
	}

	// Set Accept header to request SSE stream for video generation
	headers["Accept"] = "text/event-stream"

	// Make streaming request for SSE
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/video-model",
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}
	defer httpResp.Body.Close() //nolint:errcheck

	// Read and parse the SSE stream
	result, err := m.readSSEVideoResponse(ctx, httpResp.Body)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	// Set response metadata from the HTTP response
	responseHeaders := make(map[string]string)
	for k, vs := range httpResp.Header {
		if len(vs) > 0 {
			responseHeaders[k] = vs[0]
		}
	}
	result.Response = provider.VideoModelV3ResponseInfo{
		Timestamp: time.Now(),
		ModelID:   m.modelID,
		Headers:   responseHeaders,
	}

	return result, nil
}

// SSEVideoEvent represents a result or error event in the gateway video SSE stream.
type SSEVideoEvent struct {
	Type string `json:"type"`

	// Heartbeat fields (type="heartbeat")
	Timestamp *int64 `json:"timestamp,omitempty"`

	// Progress fields (type="progress")
	Percent *int `json:"percent,omitempty"`

	// Result fields (type="result") — the current gateway SSE completion format
	Videos           []videoData            `json:"videos,omitempty"`
	Warnings         []warningData          `json:"warnings,omitempty"`
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// Error fields (type="error")
	Message    string      `json:"message,omitempty"`
	ErrorType  string      `json:"errorType,omitempty"`
	StatusCode *int        `json:"statusCode,omitempty"`
	Param      interface{} `json:"param,omitempty"`
}

// readSSEVideoResponse reads the first data event from an SSE stream.
// Like the TypeScript SDK parser, only result and error events are accepted.
// Context cancellation is checked on each iteration to stop reading immediately.
func (m *VideoModel) readSSEVideoResponse(ctx context.Context, body io.Reader) (*provider.VideoModelV3Response, error) {
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		// Check for context cancellation before processing each line
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		// SSE data lines begin with "data: "
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var event SSEVideoEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, fmt.Errorf("failed to parse video SSE event: %w", err)
		}

		switch event.Type {
		case "result":
			// Current completion format from the gateway SSE spec
			return m.buildResponseFromSSEEvent(&event)

		case "error":
			msg := event.Message
			if msg == "" {
				msg = "unknown gateway video generation error"
			}
			statusCode := 0
			if event.StatusCode != nil {
				statusCode = *event.StatusCode
			}
			return nil, providererrors.NewProviderError("gateway", statusCode, event.ErrorType, msg, nil)
		default:
			return nil, fmt.Errorf("failed to parse video SSE event: unsupported event type %q", event.Type)
		}
	}

	// Check if the scanner stopped due to context cancellation or a read error
	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		return nil, fmt.Errorf("SSE stream read error: %w", err)
	}

	// Stream ended without a result or error event
	return nil, fmt.Errorf("SSE stream ended without a data event")
}

// buildResponseFromSSEEvent converts a result SSEVideoEvent into a VideoModelV3Response.
func (m *VideoModel) buildResponseFromSSEEvent(event *SSEVideoEvent) (*provider.VideoModelV3Response, error) {
	result := &provider.VideoModelV3Response{
		Videos:           make([]provider.VideoModelV3VideoData, 0, len(event.Videos)),
		Warnings:         make([]types.Warning, 0, len(event.Warnings)),
		ProviderMetadata: event.ProviderMetadata,
	}

	for _, video := range event.Videos {
		result.Videos = append(result.Videos, provider.VideoModelV3VideoData{
			Type:      video.Type,
			URL:       video.URL,
			Data:      video.Data,
			MediaType: video.MediaType,
		})
	}

	for _, warning := range event.Warnings {
		result.Warnings = append(result.Warnings, types.Warning{
			Type:    warning.Type,
			Feature: warning.Feature,
			Setting: warning.Setting,
			Details: warning.Details,
			Message: warning.Message,
		})
	}

	return result, nil
}

// videoData represents video data in the API response
type videoData struct {
	Type      string `json:"type"` // "url" or "base64"
	URL       string `json:"url,omitempty"`
	Data      string `json:"data,omitempty"`
	MediaType string `json:"mediaType"`
}

// warningData represents a warning in the API response
type warningData struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Feature string `json:"feature,omitempty"`
	Setting string `json:"setting,omitempty"`
	Details string `json:"details,omitempty"`
}

// encodeVideoFile encodes a video file for transmission.
// URL-type files are sent as-is as a full object {type, url}.
// Binary file data is base64-encoded; the rest of the object is preserved.
func (m *VideoModel) encodeVideoFile(file *provider.VideoModelV3File) (interface{}, error) {
	if file.Type == "url" {
		result := map[string]interface{}{
			"type": "url",
			"url":  file.URL,
		}
		if file.MediaType != "" {
			result["mediaType"] = file.MediaType
		}
		return result, nil
	}

	if file.Type == "file" && file.Data != nil {
		encoded := base64.StdEncoding.EncodeToString(file.Data)
		result := map[string]interface{}{
			"type": "file",
			"data": encoded,
		}
		if file.MediaType != "" {
			result["mediaType"] = file.MediaType
		}
		return result, nil
	}

	return nil, fmt.Errorf("invalid video file: must have either URL or binary data")
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *VideoModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-video-model-specification-version": "4",
		"ai-model-id":                          m.modelID,
	}
}

// handleError converts errors to appropriate provider errors
func (m *VideoModel) handleError(err error) error {
	return m.handleErrorWithContext(context.Background(), err)
}

func (m *VideoModel) handleErrorWithContext(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's a timeout error and convert to GatewayTimeoutError
	if gatewayerrors.IsTimeoutError(err) {
		return gatewayerrors.ConvertToGatewayTimeoutError(err, "gateway")
	}

	if gatewayerrors.IsGatewayError(err) {
		return err
	}

	// Check if it's already a provider error
	if providererrors.IsProviderError(err) {
		return err
	}

	var httpStatusErr *internalhttp.HTTPStatusError
	if errors.As(err, &httpStatusErr) {
		return m.provider.gatewayAPIErrorWithContext(ctx, &internalhttp.Response{
			StatusCode: httpStatusErr.StatusCode,
			Headers:    httpStatusErr.Headers,
			Body:       httpStatusErr.Body,
		})
	}

	return m.provider.gatewayUnknownError(err)
}
