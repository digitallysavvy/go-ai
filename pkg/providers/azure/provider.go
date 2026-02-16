package azure

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Azure OpenAI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Azure OpenAI provider
type Config struct {
	// APIKey is the Azure OpenAI API key
	APIKey string

	// ResourceName is the name of your Azure OpenAI resource
	ResourceName string

	// DeploymentID is the deployment ID of your model
	// Note: Azure uses deployments instead of model IDs
	DeploymentID string

	// APIVersion is the Azure OpenAI API version (default: 2024-02-15-preview)
	APIVersion string

	// BaseURL is an optional custom endpoint (if not using standard Azure endpoint)
	BaseURL string
}

// New creates a new Azure OpenAI provider with the given configuration
func New(cfg Config) *Provider {
	// Build base URL if not provided
	baseURL := cfg.BaseURL
	if baseURL == "" {
		// Standard Azure OpenAI endpoint format
		baseURL = fmt.Sprintf("https://%s.openai.azure.com", cfg.ResourceName)
	}

	// Default API version if not specified
	apiVersion := cfg.APIVersion
	if apiVersion == "" {
	}

	// Create HTTP client with API key header
	headers := map[string]string{
		"api-key": cfg.APIKey,
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "azure-openai"
}

// LanguageModel returns a language model by ID
// Note: For Azure, the modelID is typically the deployment name
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Use the configured deployment ID if no modelID specified
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}

	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	return NewLanguageModel(p, deploymentID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	// Use the configured deployment ID if no modelID specified
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}

	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	return NewEmbeddingModel(p, deploymentID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}

	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	return NewImageModel(p, deploymentID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}

	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	return NewSpeechModel(p, deploymentID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}

	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	return NewTranscriptionModel(p, deploymentID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Azure OpenAI doesn't provide reranking models
	return nil, fmt.Errorf("Azure OpenAI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// APIVersion returns the configured API version
func (p *Provider) APIVersion() string {
	return p.config.APIVersion
}
