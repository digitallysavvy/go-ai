package anthropic

import (
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default Anthropic API base URL. Like the TS SDK,
	// the base URL includes the /v1 version prefix; request paths are
	// relative to it ("/messages", "/files", "/skills").
	DefaultBaseURL = "https://api.anthropic.com/v1"

	// anthropicAPIURL is the unversioned API host. A base URL equal to it is
	// normalized to DefaultBaseURL.
	anthropicAPIURL = "https://api.anthropic.com"

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

	// BaseURL is the URL prefix for API calls, including the version path
	// (default: https://api.anthropic.com/v1, or ANTHROPIC_BASE_URL). The bare
	// host https://api.anthropic.com is normalized to .../v1. A base URL that
	// is set but empty after trimming whitespace makes New panic, matching the
	// TS validateBaseURL error ("baseURL must be a non-empty string.").
	BaseURL string

	// APIVersion is the Anthropic API version (default: 2023-06-01)
	APIVersion string

	// OmitAPIVersionHeader disables the anthropic-version HTTP header. This is
	// used by Vertex Anthropic, which sends anthropic_version in the JSON body.
	OmitAPIVersionHeader bool

	// HTTPClient overrides the HTTP client used for all requests.
	HTTPClient *stdhttp.Client `json:"-"`

	// MessagesPath builds the request path for messages API calls. Defaults to
	// "/messages" (relative to BaseURL).
	MessagesPath func(modelID string, stream bool) string `json:"-"`

	// TransformRequestBody can rewrite the Anthropic messages request body before
	// it is sent. It is used by Vertex Anthropic to remove the model field and
	// inject anthropic_version in the JSON body.
	TransformRequestBody func(body map[string]interface{}, stream bool) map[string]interface{} `json:"-"`

	// TransformRequestBodyWithBetas rewrites the request body with access to
	// the request's anthropic-beta flags (TS transformRequestBody(args, betas)).
	// It runs before TransformRequestBody. Bedrock-Anthropic uses it.
	TransformRequestBodyWithBetas func(body map[string]interface{}, betas []string, stream bool) map[string]interface{} `json:"-"`

	// TransformStreamBody wraps the streaming response body before SSE
	// parsing (for example to convert an AWS event stream into SSE).
	TransformStreamBody func(body io.ReadCloser, header stdhttp.Header) io.ReadCloser `json:"-"`

	// TransformErrorBody rewrites a non-2xx response body into the Anthropic
	// error shape before it is parsed.
	TransformErrorBody func(body []byte) []byte `json:"-"`

	// SupportsNativeStructuredOutput gates native structured output. A nil
	// value means true; the model capability must also allow it.
	SupportsNativeStructuredOutput *bool

	// SupportsImageInput overrides model capability detection. A nil value
	// preserves the default Anthropic model-based behavior.
	SupportsImageInput *bool

	// SupportsStrictTools controls whether strict mode on function tools is sent
	// to Anthropic. A nil value means true; the model capability must also
	// allow it.
	SupportsStrictTools *bool

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Anthropic provider with the given configuration
func New(cfg Config) *Provider {
	baseURL, err := NormalizeBaseURL(cfg.BaseURL)
	if err != nil {
		panic(err)
	}
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

// NormalizeBaseURL validates and normalizes an Anthropic base URL (TS
// normalizeBaseURL): whitespace-only values are rejected, a trailing slash is
// removed and the bare https://api.anthropic.com host gains the /v1 prefix.
// An empty value falls back to ANTHROPIC_BASE_URL and otherwise returns "".
func NormalizeBaseURL(baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = os.Getenv("ANTHROPIC_BASE_URL")
		if baseURL == "" {
			return "", nil
		}
	}
	if err := providerutils.ValidateBaseURL(baseURL); err != nil {
		return "", err
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == anthropicAPIURL {
		return DefaultBaseURL, nil
	}
	return baseURL, nil
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
