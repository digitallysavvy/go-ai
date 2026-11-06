package azure

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
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
	if isMAIImageModel(m.deploymentID) {
		return 1
	}
	return openai.ImageMaxImagesPerCall(m.deploymentID)
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

// openaiDelegate builds the OpenAI image model configured for this Azure
// deployment (TS wraps OpenAIImageModel with provider "azure.image"), so
// provider options, edits/masks, multiple images, usage and metadata match.
func (m *ImageModel) openaiDelegate() *openai.ImageModel {
	return openai.NewImageModel(m.provider.openaiDelegate(m.deploymentID), m.deploymentID)
}

func (m *ImageModel) doGenerateOpenAI(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	return m.openaiDelegate().DoGenerate(ctx, opts)
}
