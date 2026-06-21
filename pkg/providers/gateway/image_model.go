package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

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

// MaxImagesPerCall returns the maximum number of images accepted in one request.
func (m *ImageModel) MaxImagesPerCall() int {
	return 9007199254740991
}

// DoGenerate generates an image based on the given options
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Build request body
	body := map[string]interface{}{}
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}

	body["prompt"] = opts.Prompt

	if opts.N != nil {
		body["n"] = *opts.N
	}

	if opts.Size != "" {
		body["size"] = opts.Size
	}

	if opts.AspectRatio != "" {
		body["aspectRatio"] = opts.AspectRatio
	}

	if opts.Seed != nil && *opts.Seed != 0 {
		body["seed"] = *opts.Seed
	}

	if opts.ProviderOptions != nil {
		body["providerOptions"] = opts.ProviderOptions
	}

	if opts.Files != nil {
		files := make([]map[string]interface{}, 0, len(opts.Files))
		for _, file := range opts.Files {
			files = append(files, gatewayImageFile(file))
		}
		body["files"] = files
	}

	if opts.Mask != nil {
		body["mask"] = gatewayImageFile(*opts.Mask)
	}

	// Add headers
	headers := m.getModelConfigHeaders()
	for k, v := range opts.Headers {
		headers[k] = v
	}

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)

	// Make API request
	var response gatewayImageResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/image-model",
		Body:    body,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	return m.convertResponse(response, httpResp.Headers), nil
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *ImageModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-image-model-specification-version": "4",
		"ai-model-id":                          m.modelID,
	}
}

type gatewayImageResponse struct {
	Images           []string               `json:"images"`
	Warnings         []types.Warning        `json:"warnings,omitempty"`
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`
	Usage            *struct {
		InputTokens  *int `json:"inputTokens"`
		OutputTokens *int `json:"outputTokens"`
		TotalTokens  *int `json:"totalTokens"`
	} `json:"usage,omitempty"`
}

func (m *ImageModel) convertResponse(response gatewayImageResponse, headers http.Header) *types.ImageResult {
	images := make([][]byte, 0, len(response.Images))
	for _, image := range response.Images {
		images = append(images, []byte(image))
	}
	warnings := response.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	result := &types.ImageResult{
		Images:           images,
		Base64Images:     response.Images,
		MimeType:         "image/png",
		Warnings:         warnings,
		ProviderMetadata: response.ProviderMetadata,
		Response:         &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: flattenHeaders(headers)},
		Usage:            types.ImageUsage{ImageCount: len(response.Images)},
	}
	if len(images) > 0 {
		result.Image = images[0]
		result.Base64Image = response.Images[0]
	}
	if response.Usage != nil {
		if response.Usage.InputTokens != nil {
			result.Usage.InputTokens = *response.Usage.InputTokens
		}
		if response.Usage.OutputTokens != nil {
			result.Usage.OutputTokens = *response.Usage.OutputTokens
		}
		if response.Usage.TotalTokens != nil {
			result.Usage.TotalTokens = *response.Usage.TotalTokens
		}
	}
	return result
}

func gatewayImageFile(file provider.ImageFile) map[string]interface{} {
	result := map[string]interface{}{}
	if file.Type != "" {
		result["type"] = file.Type
	}
	if file.URL != "" {
		result["url"] = file.URL
	}
	if file.Data != nil {
		result["data"] = base64.StdEncoding.EncodeToString(file.Data)
	}
	if file.MediaType != "" {
		result["mediaType"] = file.MediaType
	}
	return result
}

func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
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
