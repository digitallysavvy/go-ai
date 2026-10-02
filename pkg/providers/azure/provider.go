package azure

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"net/url"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Azure OpenAI
type Provider struct {
	config     Config
	client     *http.Client
	httpClient *stdhttp.Client
	urlInfo    azureBaseURLInfo
}

// azureBaseURLInfo classifies an Azure OpenAI base URL the same way the
// TypeScript SDK's getAzureOpenAIBaseURLInfo does, so URL/query construction
// can decide whether to append "/v1" and "api-version".
type azureBaseURLInfo struct {
	// isAzureOpenAI is true when the base URL's host is a recognized Azure
	// OpenAI / AI Foundry / Cognitive Services host (or when no base URL was
	// given, i.e. the default resource-based URL is used).
	isAzureOpenAI bool
	// isFoundryProject is true for Azure AI Foundry project base URLs
	// (host ends in .services.ai.azure.com and the path starts with
	// /api/projects/). Foundry URLs never get an api-version query param.
	isFoundryProject bool
	// isVersioned is true when an Azure OpenAI base URL already ends in
	// "/openai/v1" (case-insensitive) -- the caller owns versioning already.
	isVersioned bool
}

// getAzureOpenAIBaseURLInfo mirrors the TypeScript
// getAzureOpenAIBaseURLInfo(baseURL) helper in azure-openai-provider.ts.
func getAzureOpenAIBaseURLInfo(baseURL string) azureBaseURLInfo {
	if baseURL == "" {
		return azureBaseURLInfo{isAzureOpenAI: true}
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		// Match the TS behavior of `new URL(baseURL)` throwing: fall back to
		// treating it like the default (Azure) case rather than panicking.
		return azureBaseURLInfo{isAzureOpenAI: true}
	}

	// WHATWG URL parsing (JS `new URL(...)`) lowercases the host during
	// parsing, so TS's suffix checks are implicitly case-insensitive. Go's
	// net/url does not lowercase the host, so do it explicitly to match.
	hostname := strings.ToLower(u.Hostname())
	isAzureOpenAI := strings.HasSuffix(hostname, ".openai.azure.com") ||
		strings.HasSuffix(hostname, ".services.ai.azure.com") ||
		strings.HasSuffix(hostname, ".cognitiveservices.azure.com")
	pathname := strings.TrimRight(u.Path, "/")

	isFoundryProject := strings.HasSuffix(hostname, ".services.ai.azure.com") &&
		strings.HasPrefix(pathname, "/api/projects/")
	isVersioned := isAzureOpenAI && strings.HasSuffix(strings.ToLower(pathname), "/openai/v1")

	return azureBaseURLInfo{
		isAzureOpenAI:    isAzureOpenAI,
		isFoundryProject: isFoundryProject,
		isVersioned:      isVersioned,
	}
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

	// SpeechBaseURL is the URL prefix for Azure Speech (MAI-Transcribe
	// transcription and MAI-Voice speech), e.g. a regional endpoint like
	// "https://eastus.api.cognitive.microsoft.com". Defaults to
	// "https://{ResourceName}.cognitiveservices.azure.com". Speech requests
	// do not use BaseURL or APIVersion.
	SpeechBaseURL string

	// MaiBaseURL is the URL prefix for MAI realtime APIs
	// (MAI-Transcribe-2-Streaming). Defaults to
	// "https://{ResourceName}.services.ai.azure.com/mai/v1".
	MaiBaseURL string
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
		if err := validateAzureResourceName(cfg.ResourceName); err != nil {
			return nil, err
		}
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
			SpeechBaseURL:          cfg.SpeechBaseURL,
			MaiBaseURL:             cfg.MaiBaseURL,
		},
		client:     client,
		httpClient: httpClient,
		urlInfo:    getAzureOpenAIBaseURLInfo(cfg.BaseURL),
	}, nil
}

// validateAzureResourceName rejects a resourceName that would rewrite the
// request host. resourceName is interpolated directly into the request host
// (https://{resourceName}.openai.azure.com/... and, for Azure Speech,
// https://{resourceName}.cognitiveservices.azure.com/...), so a value that
// isn't a single DNS label -- e.g. "user@internal:8080/#" -- could steer the
// request to an attacker-controlled host. Mirrors TS createAzure's
// getResourceName validation (ports TS #21640, #21842).
func validateAzureResourceName(resourceName string) error {
	if !providerutils.IsValidHostnamePart(resourceName) {
		return &providererrors.InvalidArgumentError{
			Field:   "resourceName",
			Message: "Invalid Azure resource name. Expected a single DNS label (letters, digits, and hyphens). Use `BaseURL` for custom endpoints.",
		}
	}
	return nil
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
	supportsPenaltySampling := true
	supportsStructuredOutputs := true
	chatPath := "/chat/completions"
	if p.includeAPIVersionQuery() {
		chatPath += "?api-version=" + p.config.APIVersion
	}
	deepseekProvider := deepseek.New(deepseek.Config{
		BaseURL:                   p.responsesBaseURL(modelID),
		Headers:                   p.staticAuthHeaders(),
		Name:                      "azure.deepseek",
		ProviderOptionsName:       "azure",
		ChatCompletionsPath:       chatPath,
		SupportsThinking:          &supportsThinking,
		SupportsPenaltySampling:   &supportsPenaltySampling,
		SupportsStructuredOutputs: &supportsStructuredOutputs,
		HTTPClient:                p.httpClient,
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

	var completionQuery map[string]string
	if p.includeAPIVersionQuery() {
		completionQuery = map[string]string{"api-version": p.config.APIVersion}
	}
	completionProvider := openai.New(openai.Config{
		Name:                          "azure",
		BaseURL:                       p.responsesBaseURL(modelID),
		Headers:                       headers,
		CompletionProviderName:        "azure.completion",
		CompletionProviderOptionsName: "azure",
		CompletionQuery:               completionQuery,
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

	var responsesQuery map[string]string
	if p.includeAPIVersionQuery() {
		responsesQuery = map[string]string{"api-version": p.config.APIVersion}
	}
	responsesProvider := openai.New(openai.Config{
		Name:                         "azure",
		BaseURL:                      p.responsesBaseURL(modelID),
		Headers:                      headers,
		ResponsesProviderName:        "azure.responses",
		ResponsesProviderOptionsName: "azure",
		ResponsesQuery:               responsesQuery,
		FileIDPrefixes:               []string{"assistant-"},
		ExplicitMessageItemType:      p.urlInfo.isFoundryProject,
		HTTPClient:                   p.httpClient,
	})

	return openai.NewResponsesLanguageModel(responsesProvider, modelID), nil
}

// Responses returns an Azure OpenAI Responses API language model.
func (p *Provider) Responses(modelID string) (provider.LanguageModel, error) {
	return p.ResponsesModel(modelID)
}

// responsesBaseURL builds the base URL prefix used by the openai-package
// backed models (Responses, Completion, DeepSeek). It mirrors the `url()`
// helper in azure-openai-provider.ts:
//   - useDeploymentBasedUrls: append /deployments/{modelID}
//   - non-Azure host, or an already-versioned Azure "/openai/v1" host: used
//     as-is (the caller owns routing/versioning)
//   - otherwise (a bare Azure OpenAI host): append /v1
func (p *Provider) responsesBaseURL(modelID string) string {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.openai.azure.com/openai", p.config.ResourceName)
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if p.config.UseDeploymentBasedURLs {
		return fmt.Sprintf("%s/deployments/%s", baseURL, modelID)
	}
	if !p.urlInfo.isAzureOpenAI || p.urlInfo.isVersioned {
		return baseURL
	}
	return baseURL + "/v1"
}

// includeAPIVersionQuery reports whether an "api-version" query parameter
// should be appended, mirroring TypeScript's:
//
//	options.useDeploymentBasedUrls || (isAzureOpenAI && !isVersioned && !isFoundryProject)
func (p *Provider) includeAPIVersionQuery() bool {
	return p.config.UseDeploymentBasedURLs ||
		(p.urlInfo.isAzureOpenAI && !p.urlInfo.isVersioned && !p.urlInfo.isFoundryProject)
}

// endpointPath builds the request path (plus query string) for the
// Go-native Azure models (chat, embeddings, images, speech, transcription)
// that share p.client, whose BaseURL never has "/v1" pre-appended.
func (p *Provider) endpointPath(modelID, apiPath string) string {
	var path string
	switch {
	case p.config.UseDeploymentBasedURLs:
		path = fmt.Sprintf("/deployments/%s%s", modelID, apiPath)
	case !p.urlInfo.isAzureOpenAI || p.urlInfo.isVersioned:
		path = apiPath
	default:
		path = "/v1" + apiPath
	}
	if p.includeAPIVersionQuery() {
		return path + "?api-version=" + p.config.APIVersion
	}
	return path
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

// speechHTTPClient returns the *http.Client to use for direct Azure Speech
// REST calls (p.httpClient is nil when no ADTokenProvider and no custom
// HTTPClient were configured -- internalhttp.Client handles that nil
// internally, but the raw *stdhttp.Client used by the Speech/MAI backends
// cannot, so fall back explicitly here).
func (p *Provider) speechHTTPClient() *stdhttp.Client {
	if p.httpClient != nil {
		return p.httpClient
	}
	return http.DefaultHTTPClient
}

// speechBaseURL resolves the Azure Speech endpoint prefix for MAI-Transcribe
// transcription and MAI-Voice speech, mirroring TS's `speechBaseURL()`
// closure in createAzure. ResourceName is only validated here (not
// unconditionally in New), since an explicit SpeechBaseURL makes it unused.
func (p *Provider) speechBaseURL() (string, error) {
	if p.config.SpeechBaseURL != "" {
		return strings.TrimRight(p.config.SpeechBaseURL, "/"), nil
	}
	if err := validateAzureResourceName(p.config.ResourceName); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s.cognitiveservices.azure.com", p.config.ResourceName), nil
}

// maiBaseURL resolves the MAI realtime API endpoint prefix for
// MAI-Transcribe-2-Streaming, mirroring TS's maiBaseURL provider setting
// default. ResourceName is only validated here, for the same reason as
// speechBaseURL.
func (p *Provider) maiBaseURL() (string, error) {
	if p.config.MaiBaseURL != "" {
		return strings.TrimRight(p.config.MaiBaseURL, "/"), nil
	}
	if err := validateAzureResourceName(p.config.ResourceName); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s.services.ai.azure.com/mai/v1", p.config.ResourceName), nil
}

// speechAPIHeaders builds headers for the Azure Speech REST APIs
// (transcription and TTS), mirroring TS getHeaders('speech'). REST calls run
// through p.httpClient, whose azureTokenTransport already injects
// "Authorization: Bearer <token>" when ADTokenProvider is set, so no key
// header is added in that case.
func (p *Provider) speechAPIHeaders() map[string]string {
	headers := map[string]string{}
	if p.config.ADTokenProvider == nil {
		headers["Ocp-Apim-Subscription-Key"] = p.config.APIKey
	}
	return version.WithUserAgentSuffix(http.MergeHeaders(headers, p.config.Headers), version.ProviderUserAgent("azure"))
}

// maiAPIHeaders builds headers for the MAI realtime WebSocket, mirroring TS
// getHeaders('mai'). Unlike the REST backends, a WebSocket dial does not go
// through p.httpClient's transport, so the bearer token is resolved and set
// explicitly here when ADTokenProvider is configured.
func (p *Provider) maiAPIHeaders(ctx context.Context) (map[string]string, error) {
	headers := map[string]string{}
	if p.config.ADTokenProvider != nil {
		token, err := p.config.ADTokenProvider(ctx)
		if err != nil {
			return nil, err
		}
		headers["authorization"] = "Bearer " + token
	} else {
		headers["api-key"] = p.config.APIKey
	}
	return version.WithUserAgentSuffix(http.MergeHeaders(headers, p.config.Headers), version.ProviderUserAgent("azure")), nil
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

// SpeechModel returns a speech synthesis model by ID. MAI-Voice models use
// the Azure Speech API by default; other IDs use OpenAI. Override with
// providerOptions.azure.api.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return newAzureSpeechDispatchModel(
		modelID,
		p.config,
		NewSpeechModel(p, modelID),
		newAzureSpeechSpeechModel(modelID,
			func() (string, error) {
				base, err := p.speechBaseURL()
				if err != nil {
					return "", err
				}
				return base + "/tts/cognitiveservices/v1", nil
			},
			func(context.Context) (map[string]string, error) { return p.speechAPIHeaders(), nil },
			p.speechHTTPClient(),
		),
	), nil
}

// Speech returns a speech synthesis model by ID.
func (p *Provider) Speech(modelID string) (provider.SpeechModel, error) {
	return p.SpeechModel(modelID)
}

// TranscriptionModel returns a speech-to-text model by ID. MAI-Transcribe
// models use the Azure Speech API, and MAI-Transcribe-2-Streaming the MAI
// realtime API, by default; other IDs use OpenAI. Override with
// providerOptions.azure.api.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return newAzureTranscriptionDispatchModel(
		modelID,
		p.config,
		NewTranscriptionModel(p, modelID),
		newAzureSpeechTranscriptionModel(modelID,
			func() (string, error) {
				base, err := p.speechBaseURL()
				if err != nil {
					return "", err
				}
				return base + "/speechtotext/transcriptions:transcribe?api-version=2025-10-15", nil
			},
			func(context.Context) (map[string]string, error) { return p.speechAPIHeaders(), nil },
			p.speechHTTPClient(),
		),
		newAzureMaiTranscriptionModel(modelID,
			func() (string, error) {
				base, err := p.maiBaseURL()
				if err != nil {
					return "", err
				}
				return base + "/realtime?intent=transcription", nil
			},
			p.maiAPIHeaders,
		),
	), nil
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
