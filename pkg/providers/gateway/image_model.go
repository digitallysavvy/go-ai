package gateway

import (
	"context"
	"errors"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// ImageModel implements the provider.ImageModel interface for AI Gateway
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new AI Gateway image model
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
	return "gateway"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate generates an image based on the given options
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Build request body
	body := map[string]interface{}{}

	if opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	if opts.N != nil {
		body["n"] = *opts.N
	}

	if opts.Size != "" {
		body["size"] = opts.Size
	}

	if opts.Quality != "" {
		body["quality"] = opts.Quality
	}

	if opts.Style != "" {
		body["style"] = opts.Style
	}

	// Add headers
	headers := m.getModelConfigHeaders()

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)

	// Make API request
	var result types.ImageResult
	err := m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/image-model",
		Body:    body,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	return &result, nil
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *ImageModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-image-model-specification-version": "3",
		"ai-image-model-id":                    m.modelID,
	}
}

// handleError converts errors to appropriate provider errors
func (m *ImageModel) handleError(err error) error {
	return m.handleErrorWithContext(context.Background(), err)
}

func (m *ImageModel) handleErrorWithContext(ctx context.Context, err error) error {
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
