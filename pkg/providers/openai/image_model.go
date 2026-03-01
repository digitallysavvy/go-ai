package openai

import (
	"context"
	"encoding/base64"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for OpenAI DALL-E
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new OpenAI image generation model
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
	return "openai"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)
	var response openaiImageResponse
	err := m.provider.client.PostJSON(ctx, "/v1/images/generations", reqBody, &response)
	if err != nil {
		return nil, providererrors.NewProviderError("openai", 0, "", err.Error(), err)
	}
	return m.convertResponse(response)
}

// defaultResponseFormatPrefixes lists model ID prefixes that have their own
// default response format and should not receive an explicit "response_format"
// override. Setting response_format: "b64_json" on these models causes errors
// because they use a different default format (#12838).
var defaultResponseFormatPrefixes = []string{
	"chatgpt-image-",
	"gpt-image-1-mini",
	"gpt-image-1.5",
	"gpt-image-1",
}

// hasDefaultResponseFormat returns true when the model has its own built-in
// response format and must not be sent an explicit response_format field.
func hasDefaultResponseFormat(modelID string) bool {
	for _, prefix := range defaultResponseFormatPrefixes {
		if len(modelID) >= len(prefix) && modelID[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	// Only set response_format for models that don't have a built-in default.
	// chatgpt-image and gpt-image-1 variants manage their own format (#12838).
	if !hasDefaultResponseFormat(m.modelID) {
		body["response_format"] = "b64_json"
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
	return body
}

func (m *ImageModel) convertResponse(response openaiImageResponse) (*types.ImageResult, error) {
	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no image data returned from OpenAI")
	}
	imageData := response.Data[0]
	imageBytes, err := base64.StdEncoding.DecodeString(imageData.B64JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 image: %w", err)
	}
	return &types.ImageResult{
		Image:    imageBytes,
		MimeType: "image/png",
		URL:      imageData.URL,
		Usage: types.ImageUsage{
			ImageCount: len(response.Data),
		},
	}, nil
}

type openaiImageResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON         string `json:"b64_json"`
		URL             string `json:"url"`
		RevisedPrompt   string `json:"revised_prompt"`
	} `json:"data"`
}
