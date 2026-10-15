package azure

import (
	"context"
	"encoding/json"
	"fmt"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for Azure image
// generation. MAI-Image models use the MAI image API by default; other IDs
// use the OpenAI images API. Override per request with
// providerOptions.azure.api ("openai" or "mai"), mirroring TS AzureImageModel.
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
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "azure.image"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *ImageModel) ModelID() string {
	return m.deploymentID
}

// MaxImagesPerCall returns the maximum number of images accepted in one call.
func (m *ImageModel) MaxImagesPerCall() int {
	return 1
}

// SupportsFileInputs follows the default route: MAI-Image models accept
// reference images; for other IDs support is unknown, because Azure model IDs
// are user-defined deployment names and OpenAI model family capabilities
// cannot be inferred from them. A per-request `api` override does not change
// it.
func (m *ImageModel) SupportsFileInputs() *bool {
	if isMAIImageModel(m.deploymentID) {
		v := true
		return &v
	}
	return nil
}

// SupportsMaskInputs follows the default route: MAI image edits do not
// support masks; unknown for other IDs, for the same reason as
// SupportsFileInputs.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if isMAIImageModel(m.deploymentID) {
		v := false
		return &v
	}
	return nil
}

// DoGenerate performs image generation, routing to the MAI or OpenAI image
// API per providerOptions.azure.api (default by model ID).
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	azureOpts, err := parseAzureImageProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	api := azureOpts.API
	if api == "" {
		if isMAIImageModel(m.deploymentID) {
			api = "mai"
		} else {
			api = "openai"
		}
	}
	if api == "mai" {
		mai := &azureMaiImageModel{
			modelID: m.deploymentID,
			url: func(path string) (string, error) {
				base, err := m.provider.maiBaseURL()
				if err != nil {
					return "", err
				}
				return base + path, nil
			},
			headers:    m.provider.maiAPIHeaders,
			httpClient: m.provider.speechHTTPClient(),
		}
		return mai.doGenerate(ctx, opts, azureOpts)
	}

	result, err := m.doGenerateOpenAI(ctx, opts)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, azureMaiImageOnlyWarnings(azureOpts)...)
	return result, nil
}

func (m *ImageModel) doGenerateOpenAI(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	reqBody := m.buildRequestBody(opts)

	// Azure OpenAI image generation endpoint
	path := m.provider.endpointPath(m.deploymentID, "/images/generations")
	headers, err := m.provider.requestHeaders(ctx, opts.Headers)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    path,
		Body:    reqBody,
		Headers: headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, internalhttp.StatusProviderError("azure", resp.StatusCode, resp.Headers, resp.Body)
	}

	return m.convertResponse(resp.Body)
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"prompt": opts.Prompt,
		"model":  m.deploymentID,
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
