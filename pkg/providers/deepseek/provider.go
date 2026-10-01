package deepseek

import (
	"fmt"
	stdhttp "net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Deepseek
type Provider struct {
	config  Config
	client  *http.Client
	baseURL string
}

// Config contains configuration for the Deepseek provider
type Config struct {
	// APIKey is the Deepseek API key
	APIKey string

	// BaseURL is the base URL for the Deepseek API (optional)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// Name overrides the provider name surfaced by language models.
	Name string

	// ProviderOptionsName overrides the providerOptions namespace.
	ProviderOptionsName string

	// ChatCompletionsPath overrides the request path.
	ChatCompletionsPath string

	// SupportsThinking controls whether top-level reasoning also sends the
	// DeepSeek thinking field. Defaults to true.
	SupportsThinking *bool

	// SupportsPenaltySampling controls whether frequency_penalty/presence_penalty
	// are sent to the API. The upstream DeepSeek API deprecated these fields,
	// but Azure-hosted DeepSeek deployments still accept them. Defaults to
	// false (matching TypeScript's `this.config.supportsPenaltySampling === true`).
	SupportsPenaltySampling *bool

	// SupportsStructuredOutputs controls whether a JSON response format with a
	// schema is sent as `response_format:{type:"json_schema",...}` instead of
	// `response_format:{type:"json_object"}`. Azure-hosted DeepSeek deployments
	// support this; the upstream DeepSeek API does not. Defaults to false.
	SupportsStructuredOutputs *bool

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *stdhttp.Client `json:"-"`
}

// New creates a new Deepseek provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}

	mergedHeaders := http.MergeHeaders(headers, cfg.Headers)
	// Azure-hosted DeepSeek (pkg/providers/azure.DeepSeekModel) reuses this
	// constructor but already tagged mergedHeaders with its own
	// `ai-sdk/azure/VERSION` (see azure's staticAuthHeaders); only add the
	// standalone-DeepSeek tag when no wrapping caller has already tagged it,
	// matching TS deepseek-provider.ts's own `ai-sdk/deepseek/VERSION`.
	if !providerutils.HasUserAgent(mergedHeaders) {
		mergedHeaders = version.WithUserAgentSuffix(mergedHeaders, version.ProviderUserAgent("deepseek"))
	}

	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    mergedHeaders,
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{
		config:  cfg,
		client:  client,
		baseURL: baseURL,
	}
}

// CreateDeepSeek creates a new DeepSeek provider.
//
// It mirrors the TypeScript SDK createDeepSeek export while New remains the
// idiomatic Go constructor.
func CreateDeepSeek(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "deepseek"
}

func (p *Provider) providerOptionsName() string {
	if p.config.ProviderOptionsName != "" {
		return p.config.ProviderOptionsName
	}
	return "deepseek"
}

func (p *Provider) chatCompletionsPath() string {
	if p.config.ChatCompletionsPath != "" {
		return p.config.ChatCompletionsPath
	}
	return "/v1/chat/completions"
}

func (p *Provider) supportsThinking() bool {
	if p.config.SupportsThinking == nil {
		return true
	}
	return *p.config.SupportsThinking
}

func (p *Provider) supportsPenaltySampling() bool {
	return p.config.SupportsPenaltySampling != nil && *p.config.SupportsPenaltySampling
}

func (p *Provider) supportsStructuredOutputs() bool {
	return p.config.SupportsStructuredOutputs != nil && *p.config.SupportsStructuredOutputs
}

// supportsBeta reports whether the configured base URL points at DeepSeek's
// beta endpoint (a base URL ending in "/beta"). Several DeepSeek features
// (assistant prefix completion, strict tool calls) are only available there.
func (p *Provider) supportsBeta() bool {
	return strings.HasSuffix(p.baseURL, "/beta")
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "deepseek-chat"
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Deepseek does not support embeddings") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("Deepseek does not support image generation") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Deepseek does not support speech synthesis") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Deepseek does not support transcription") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Deepseek does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// Files returns the DeepSeek Files API for uploading images referenced by
// file_id in chat messages.
func (p *Provider) Files() provider.FilesAPI {
	return &FilesAPI{provider: p}
}
