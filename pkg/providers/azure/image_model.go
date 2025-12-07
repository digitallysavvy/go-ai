package azure

import (
	"context"
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Azure OpenAI
type ImageModel struct {
	provider     *Provider
	deploymentID string
}

// NewImageModel creates a new Azure OpenAI image generation model
func NewImageModel(provider *Provider, deploymentID string) *ImageModel {
	return &ImageModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "azure-openai"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *ImageModel) ModelID() string {
	return m.deploymentID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	// Azure OpenAI image generation endpoint
	path := fmt.Sprintf("/openai/deployments/%s/images/generations?api-version=%s",
		m.deploymentID,
		m.provider.APIVersion())

	resp, err := m.provider.client.Post(ctx, path, reqBody)
	if err != nil {
		return nil, providererrors.NewProviderError("azure-openai", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Azure OpenAI API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	return m.convertResponse(resp.Body)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	if opts.N != nil {
		reqBody["n"] = *opts.N
	}

	if opts.Size != "" {
		reqBody["size"] = opts.Size
	}

	if opts.Quality != "" {
		reqBody["quality"] = opts.Quality
	}

	if opts.Style != "" {
		reqBody["style"] = opts.Style
	}

	return reqBody
}

func (m *ImageModel) convertResponse(body []byte) (*types.ImageResult, error) {
	var response struct {
		Created int64 `json:"created"`
		Data    []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no images generated")
	}

	result := &types.ImageResult{
		Usage: types.ImageUsage{},
	}

	if response.Data[0].URL != "" {
		result.URL = response.Data[0].URL
		result.MimeType = "image/png"
	}

	if response.Data[0].B64JSON != "" {
		result.Image = []byte(response.Data[0].B64JSON)
		result.MimeType = "image/png"
	}

	return result, nil
}
