package anthropic

import (
	"fmt"
	stdhttp "net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default Anthropic API base URL
	DefaultBaseURL = "https://api.anthropic.com"

	// DefaultAPIVersion is the default Anthropic API version
	DefaultAPIVersion = "2023-06-01"
)

// Provider implements the provider.Provider interface for Anthropic
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Anthropic provider
type Config struct {
	// APIKey is the Anthropic API key
	APIKey string

	// Name overrides the provider name returned by Provider.Name() and
	// LanguageModel.Provider(). Defaults to "anthropic".
	Name string

	// BaseURL is the base URL for the Anthropic API (default: https://api.anthropic.com)
	BaseURL string

	// APIVersion is the Anthropic API version (default: 2023-06-01)
	APIVersion string

	// OmitAPIVersionHeader disables the anthropic-version HTTP header. This is
	// used by Vertex Anthropic, which sends anthropic_version in the JSON body.
	OmitAPIVersionHeader bool

	// HTTPClient overrides the HTTP client used for all requests.
	HTTPClient *stdhttp.Client `json:"-"`

	// MessagesPath builds the request path for messages API calls. Defaults to
	// "/v1/messages".
	MessagesPath func(modelID string, stream bool) string `json:"-"`

	// TransformRequestBody can rewrite the Anthropic messages request body before
	// it is sent. It is used by Vertex Anthropic to remove the model field and
	// inject anthropic_version in the JSON body.
	TransformRequestBody func(body map[string]interface{}, stream bool) map[string]interface{} `json:"-"`

	// SupportsNativeStructuredOutput overrides model capability detection. A nil
	// value preserves the default Anthropic model-based behavior.
	SupportsNativeStructuredOutput *bool

	// SupportsImageInput overrides model capability detection. A nil value
	// preserves the default Anthropic model-based behavior.
	SupportsImageInput *bool

	// SupportsStrictTools controls whether strict mode on function tools is sent
	// to Anthropic. A nil value preserves the default Anthropic behavior.
	SupportsStrictTools *bool

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Anthropic provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}

	// Create HTTP client with default headers
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["x-api-key"] = cfg.APIKey
	}
	if !cfg.OmitAPIVersionHeader {
		headers["anthropic-version"] = apiVersion
	}

	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    http.MergeHeaders(headers, cfg.Headers),
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateAnthropic creates a new Anthropic provider.
//
// It mirrors the TypeScript SDK createAnthropic export while New remains the
// idiomatic Go constructor.
func CreateAnthropic(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "anthropic"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID, nil), nil
}

// LanguageModelWithOptions returns a language model with custom options
func (p *Provider) LanguageModelWithOptions(modelID string, options *ModelOptions) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID, options), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	// Anthropic doesn't provide embedding models
	return nil, fmt.Errorf("anthropic does not support embedding models")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Anthropic doesn't provide image generation models
	return nil, fmt.Errorf("anthropic does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	// Anthropic doesn't provide speech synthesis models
	return nil, fmt.Errorf("anthropic does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Anthropic doesn't provide transcription models
	return nil, fmt.Errorf("LAnthropic does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Anthropic doesn't provide reranking models
	return nil, fmt.Errorf("LAnthropic does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

func (p *Provider) Files() provider.FilesAPI {
	return &FilesAPI{provider: p}
}

func (p *Provider) Skills() provider.SkillsAPI {
	return &SkillsAPI{provider: p}
}
