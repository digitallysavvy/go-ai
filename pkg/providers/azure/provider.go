package azure

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
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

	// APIVersion is the Azure OpenAI API version (default: v1)
	APIVersion string

	// BaseURL is an optional custom endpoint (if not using standard Azure endpoint)
	BaseURL string

	// UseDeploymentBasedURLs switches model calls to the legacy Azure deployment
	// URL shape: /deployments/{deploymentID}{path}?api-version={version}. The
	// default matches TypeScript and uses /v1{path}?api-version={version}.
	UseDeploymentBasedURLs bool

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Azure OpenAI provider with the given configuration
func New(cfg Config) *Provider {
	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = "v1"
	}

	// Build base URL if not provided
	baseURL := cfg.BaseURL
	if baseURL == "" {
		// Standard Azure OpenAI endpoint format
		baseURL = fmt.Sprintf("https://%s.openai.azure.com/openai", cfg.ResourceName)
	}

	// Create HTTP client with API key header
	headers := map[string]string{
		"api-key": cfg.APIKey,
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: http.MergeHeaders(headers, cfg.Headers),
	})

	return &Provider{
		config: Config{
			APIKey:                 cfg.APIKey,
			ResourceName:           cfg.ResourceName,
			DeploymentID:           cfg.DeploymentID,
			APIVersion:             apiVersion,
			BaseURL:                cfg.BaseURL,
			UseDeploymentBasedURLs: cfg.UseDeploymentBasedURLs,
			Headers:                cfg.Headers,
		},
		client: client,
	}
}

// CreateAzure creates a new Azure OpenAI provider.
//
// It mirrors the TypeScript SDK createAzure export while New remains the
// idiomatic Go constructor.
func CreateAzure(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "azure-openai"
}

// LanguageModel returns a Responses API language model by deployment ID, matching
// the TypeScript Azure provider's default languageModel factory.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.ResponsesModel(modelID)
}

// ChatModel returns a legacy deployment-based Chat Completions language model.
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
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

// CompletionModel returns an Azure OpenAI Completions API language model by
// deployment ID, matching the TypeScript Azure provider's completion factory.
func (p *Provider) CompletionModel(modelID string) (provider.LanguageModel, error) {
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	headers := http.MergeHeaders(map[string]string{
		"api-key": p.config.APIKey,
	}, p.config.Headers)

	completionProvider := openai.New(openai.Config{
		Name:                          "azure",
		BaseURL:                       p.responsesBaseURL(deploymentID),
		Headers:                       headers,
		CompletionProviderName:        "azure.completion",
		CompletionProviderOptionsName: "azure",
		CompletionQuery:               map[string]string{"api-version": p.config.APIVersion},
	})

	return openai.NewCompletionModel(completionProvider, deploymentID), nil
}

// ResponsesModel returns an Azure OpenAI Responses API language model by
// deployment ID. Requests use /openai/v1/responses?api-version={version}, the
// api-key header, and Azure's assistant- file ID compatibility prefix.
func (p *Provider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	deploymentID := modelID
	if deploymentID == "" {
		deploymentID = p.config.DeploymentID
	}
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is required for Azure OpenAI")
	}

	headers := http.MergeHeaders(map[string]string{
		"api-key": p.config.APIKey,
	}, p.config.Headers)

	responsesProvider := openai.New(openai.Config{
		Name:                         "azure",
		BaseURL:                      p.responsesBaseURL(deploymentID),
		Headers:                      headers,
		ResponsesProviderName:        "azure.responses",
		ResponsesProviderOptionsName: "azure",
		ResponsesQuery:               map[string]string{"api-version": p.config.APIVersion},
		FileIDPrefixes:               []string{"assistant-"},
	})

	return openai.NewResponsesLanguageModel(responsesProvider, deploymentID), nil
}

func (p *Provider) responsesBaseURL(modelID string) string {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.openai.azure.com/openai", p.config.ResourceName)
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if p.config.UseDeploymentBasedURLs {
		return fmt.Sprintf("%s/deployments/%s", baseURL, modelID)
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

func (p *Provider) endpointPath(modelID, apiPath string) string {
	if p.config.UseDeploymentBasedURLs {
		return fmt.Sprintf("/deployments/%s%s?api-version=%s", modelID, apiPath, p.config.APIVersion)
	}
	return fmt.Sprintf("/v1%s?api-version=%s", apiPath, p.config.APIVersion)
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
	return nil, fmt.Errorf("azure OpenAI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// APIVersion returns the configured API version
func (p *Provider) APIVersion() string {
	return p.config.APIVersion
}
