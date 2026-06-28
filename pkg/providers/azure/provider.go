package azure

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Azure OpenAI
type Provider struct {
	config     Config
	client     *http.Client
	httpClient *stdhttp.Client
}

// Config contains configuration for the Azure OpenAI provider
type Config struct {
	// APIKey is the Azure OpenAI API key
	APIKey string

	// ADTokenProvider returns a Microsoft Entra ID access token for each
	// request. When set, Authorization: Bearer <token> is used instead of the
	// api-key header. The request context is passed through to token acquisition.
	ADTokenProvider func(ctx context.Context) (string, error) `json:"-"`

	// ResourceName is the name of your Azure OpenAI resource
	ResourceName string

	// DeploymentID is retained for workflow compatibility with older Go configs.
	// Azure model factories preserve the explicit modelID/deploymentID argument,
	// matching the TypeScript SDK createAzure behavior.
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

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *stdhttp.Client `json:"-"`
}

// New creates a new Azure OpenAI provider with the given configuration.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey != "" && cfg.ADTokenProvider != nil {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "apiKey/tokenProvider",
			Message: "Both apiKey and tokenProvider were provided. Please use only one authentication method.",
		}
	}
	if cfg.APIKey == "" && cfg.ADTokenProvider == nil {
		cfg.APIKey = os.Getenv("AZURE_API_KEY")
	}
	if cfg.ResourceName == "" {
		cfg.ResourceName = os.Getenv("AZURE_RESOURCE_NAME")
	}

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

	headers := map[string]string{}
	if cfg.ADTokenProvider == nil {
		headers["api-key"] = cfg.APIKey
	}
	httpClient := azureHTTPClient(cfg.HTTPClient, cfg.ADTokenProvider)

	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    version.WithUserAgentSuffix(http.MergeHeaders(headers, cfg.Headers), version.ProviderUserAgent("azure")),
		HTTPClient: httpClient,
	})

	return &Provider{
		config: Config{
			APIKey:                 cfg.APIKey,
			ADTokenProvider:        cfg.ADTokenProvider,
			ResourceName:           cfg.ResourceName,
			DeploymentID:           cfg.DeploymentID,
			APIVersion:             apiVersion,
			BaseURL:                cfg.BaseURL,
			UseDeploymentBasedURLs: cfg.UseDeploymentBasedURLs,
			Headers:                cfg.Headers,
			HTTPClient:             cfg.HTTPClient,
		},
		client:     client,
		httpClient: httpClient,
	}, nil
}

// CreateAzure creates a new Azure OpenAI provider.
//
// It mirrors the TypeScript SDK createAzure export while New remains the
// idiomatic Go constructor.
func CreateAzure(cfg Config) (*Provider, error) {
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
	return NewLanguageModel(p, modelID), nil
}

// Chat returns a legacy deployment-based Chat Completions language model.
func (p *Provider) Chat(modelID string) (provider.LanguageModel, error) {
	return p.ChatModel(modelID)
}

// DeepSeekModel returns an Azure-hosted DeepSeek chat model.
func (p *Provider) DeepSeekModel(modelID string) (provider.LanguageModel, error) {
	supportsThinking := false
	deepseekProvider := deepseek.New(deepseek.Config{
		BaseURL:             p.responsesBaseURL(modelID),
		Headers:             p.staticAuthHeaders(),
		Name:                "azure.deepseek",
		ProviderOptionsName: "azure",
		ChatCompletionsPath: "/chat/completions?api-version=" + p.config.APIVersion,
		SupportsThinking:    &supportsThinking,
		HTTPClient:          p.httpClient,
	})
	return deepseek.NewLanguageModel(deepseekProvider, modelID), nil
}

// DeepSeek returns an Azure-hosted DeepSeek chat model.
func (p *Provider) DeepSeek(modelID string) (provider.LanguageModel, error) {
	return p.DeepSeekModel(modelID)
}

// CompletionModel returns an Azure OpenAI Completions API language model by
// deployment ID, matching the TypeScript Azure provider's completion factory.
func (p *Provider) CompletionModel(modelID string) (provider.LanguageModel, error) {
	headers := p.staticAuthHeaders()

	completionProvider := openai.New(openai.Config{
		Name:                          "azure",
		BaseURL:                       p.responsesBaseURL(modelID),
		Headers:                       headers,
		CompletionProviderName:        "azure.completion",
		CompletionProviderOptionsName: "azure",
		CompletionQuery:               map[string]string{"api-version": p.config.APIVersion},
		HTTPClient:                    p.httpClient,
	})

	return openai.NewCompletionModel(completionProvider, modelID), nil
}

// Completion returns an Azure OpenAI Completions API language model.
func (p *Provider) Completion(modelID string) (provider.LanguageModel, error) {
	return p.CompletionModel(modelID)
}

// ResponsesModel returns an Azure OpenAI Responses API language model by
// deployment ID. Requests use /openai/v1/responses?api-version={version}, the
// api-key header, and Azure's assistant- file ID compatibility prefix.
func (p *Provider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	headers := p.staticAuthHeaders()

	responsesProvider := openai.New(openai.Config{
		Name:                         "azure",
		BaseURL:                      p.responsesBaseURL(modelID),
		Headers:                      headers,
		ResponsesProviderName:        "azure.responses",
		ResponsesProviderOptionsName: "azure",
		ResponsesQuery:               map[string]string{"api-version": p.config.APIVersion},
		FileIDPrefixes:               []string{"assistant-"},
		HTTPClient:                   p.httpClient,
	})

	return openai.NewResponsesLanguageModel(responsesProvider, modelID), nil
}

// Responses returns an Azure OpenAI Responses API language model.
func (p *Provider) Responses(modelID string) (provider.LanguageModel, error) {
	return p.ResponsesModel(modelID)
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

func (p *Provider) staticAuthHeaders() map[string]string {
	headers := map[string]string{}
	if p.config.ADTokenProvider == nil {
		headers["api-key"] = p.config.APIKey
	}
	return version.WithUserAgentSuffix(http.MergeHeaders(headers, p.config.Headers), version.ProviderUserAgent("azure"))
}

func (p *Provider) requestHeaders(_ context.Context, extra map[string]string) (map[string]string, error) {
	return extra, nil
}

func azureHTTPClient(base *stdhttp.Client, tokenProvider func(context.Context) (string, error)) *stdhttp.Client {
	if tokenProvider == nil {
		return base
	}
	if base == nil {
		base = http.DefaultHTTPClient
	}
	clone := *base
	transport := base.Transport
	if transport == nil {
		transport = stdhttp.DefaultTransport
	}
	clone.Transport = azureTokenTransport{base: transport, tokenProvider: tokenProvider}
	return &clone
}

type azureTokenTransport struct {
	base          stdhttp.RoundTripper
	tokenProvider func(context.Context) (string, error)
}

func (t azureTokenTransport) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	clone := req.Clone(req.Context())
	if clone.Header.Get("Authorization") == "" {
		token, err := t.tokenProvider(req.Context())
		if err != nil {
			return nil, err
		}
		clone.Header.Set("Authorization", "Bearer "+token)
	}
	return t.base.RoundTrip(clone)
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return NewEmbeddingModel(p, modelID), nil
}

// Embedding returns an embedding model by ID.
func (p *Provider) Embedding(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// TextEmbedding returns an embedding model by ID.
//
// Deprecated: use Embedding.
func (p *Provider) TextEmbedding(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// TextEmbeddingModel returns an embedding model by ID.
//
// Deprecated: use EmbeddingModel.
func (p *Provider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return NewImageModel(p, modelID), nil
}

// Image returns an image generation model by ID.
func (p *Provider) Image(modelID string) (provider.ImageModel, error) {
	return p.ImageModel(modelID)
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return NewSpeechModel(p, modelID), nil
}

// Speech returns a speech synthesis model by ID.
func (p *Provider) Speech(modelID string) (provider.SpeechModel, error) {
	return p.SpeechModel(modelID)
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return NewTranscriptionModel(p, modelID), nil
}

// Transcription returns a speech-to-text model by ID.
func (p *Provider) Transcription(modelID string) (provider.TranscriptionModel, error) {
	return p.TranscriptionModel(modelID)
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
