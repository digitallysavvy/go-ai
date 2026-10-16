package anthropic

import (
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/version"
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
	// BatchResultDownloads configures JSON Lines batch result downloads, e.g.
	// the maximum bytes per result row (TS `batchResultDownloads`).
	BatchResultDownloads providerutils.BatchResultDownloads

	// APIKey is the Anthropic API key. When it is empty (and AuthToken is not
	// set) the direct Anthropic provider falls back to the ANTHROPIC_API_KEY
	// environment variable like TS loadApiKey; providers built on top of this
	// one (Vertex, Bedrock, AWS, MiniMax) never read it.
	APIKey string

	// AuthToken authenticates with an `Authorization: Bearer` header instead
	// of an API key (TS authToken). Setting both APIKey and AuthToken makes
	// New panic with an InvalidArgumentError, like the TS constructor throws.
	AuthToken string

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

	// TransformRequestHeaders rewrites the per-request headers (caller headers
	// and anthropic-beta) with access to the call's provider options (TS
	// transformRequestHeaders). Vertex Anthropic uses it to send
	// providerOptions.vertex.labels as the x-vertex-ai-labels header. A
	// returned error aborts the request before it is sent.
	TransformRequestHeaders func(headers map[string]string, providerOptions map[string]interface{}) (map[string]string, error) `json:"-"`

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

	// SupportedURLs overrides the URL patterns the model accepts directly
	// without downloading first (TS languageModelConfig.supportedUrls). A nil
	// value falls back to DefaultSupportedURLs (https image/* and
	// application/pdf), matching the direct Anthropic and anthropic-aws
	// providers. Vertex-Anthropic and Bedrock-Anthropic set this to a function
	// that returns an empty map to force base64 conversion, matching TS.
	SupportedURLs func(modelID string) map[string][]string `json:"-"`

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// GenerateID creates the ids of generated sources (TS `generateId`
	// provider setting). Defaults to a random id.
	GenerateID func() string `json:"-"`

	// UserAgentName selects the `ai-sdk-<name>/VERSION` User-Agent tag this
	// provider construction adds (version.ProviderUserAgent). Defaults to
	// "anthropic". TS's anthropic-aws and minimax packages are each their
	// own npm package with their own tag ("ai-sdk-anthropic-aws",
	// "ai-sdk-minimax"); Go's anthropicaws and minimax packages implement
	// this by reusing anthropic.New as their Messages-API transport, so they
	// set this field to avoid inheriting the wrong "ai-sdk-anthropic" tag.
	UserAgentName string `json:"userAgentName,omitempty"`

	// NoUserAgentTag disables the `ai-sdk-<name>/VERSION` tag entirely
	// (UserAgentName is ignored when this is true). TS's
	// google-vertex-anthropic-provider.ts builds its
	// AnthropicLanguageModel directly instead of going through
	// createAnthropic (the only place TS's own `ai-sdk-anthropic/VERSION`
	// tag is added), so Vertex-Anthropic requests carry no
	// anthropic-package tag at all -- only the runtime tag the shared HTTP
	// client appends downstream. pkg/providers/googlevertex/anthropic sets
	// this to match.
	NoUserAgentTag bool `json:"noUserAgentTag,omitempty"`

	// APIKeyEnvVar makes a wrapper provider (UserAgentName/NoUserAgentTag set)
	// load its key at request time like TS loadApiKey: APIKey, else this
	// environment variable, else a LoadAPIKeyError. Empty keeps the wrapper's
	// own credentials untouched (anthropicaws, Vertex, Bedrock).
	APIKeyEnvVar string `json:"-"`

	// APIKeyDescription names the wrapper in the LoadAPIKeyError message.
	APIKeyDescription string `json:"-"`
}

// DefaultSupportedURLs returns the URL patterns the direct Anthropic API and
// anthropic-aws accept without downloading first: https URLs for image and
// PDF content. Mirrors the supportedUrls map in TS anthropic-provider.ts /
// anthropic-aws-provider.ts.
func DefaultSupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*":         {`^https?://.*$`},
		"application/pdf": {`^https?://.*$`},
	}
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
	if cfg.APIKey != "" && cfg.AuthToken != "" {
		panic(&providererrors.InvalidArgumentError{
			Field:   "apiKey/authToken",
			Message: "Both apiKey and authToken were provided. Please use only one authentication method.",
		})
	}
	headers := authHeadersFor(cfg)
	if !cfg.OmitAPIVersionHeader {
		headers["anthropic-version"] = apiVersion
	}

	mergedHeaders := http.MergeHeaders(headers, cfg.Headers)
	if !cfg.NoUserAgentTag {
		uaName := cfg.UserAgentName
		if uaName == "" {
			uaName = "anthropic"
		}
		mergedHeaders = version.WithUserAgentSuffix(mergedHeaders, version.ProviderUserAgent(uaName))
	}

	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    mergedHeaders,
		HTTPClient: cfg.HTTPClient,
		// TS getHeaders runs per request: the key is (re)loaded then, and a
		// missing key fails the request with a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) { return requestAuthHeaders(cfg) },
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// authHeadersFor returns the authentication header of cfg: a bearer token when
// AuthToken is set, otherwise x-api-key (see Config.APIKey for the
// ANTHROPIC_API_KEY fallback).
func authHeadersFor(cfg Config) map[string]string {
	headers := map[string]string{}
	apiKey := cfg.APIKey
	if apiKey == "" && cfg.AuthToken == "" && cfg.UserAgentName == "" && !cfg.NoUserAgentTag {
		// The plain Anthropic provider reads ANTHROPIC_API_KEY (TS loadApiKey);
		// wrapper providers always set UserAgentName or NoUserAgentTag and must
		// not forward an Anthropic key to another service.
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if cfg.AuthToken != "" {
		headers["Authorization"] = "Bearer " + cfg.AuthToken
	} else if apiKey != "" {
		headers["x-api-key"] = apiKey
	}
	return headers
}

// requestAuthHeaders resolves the credentials of a plain Anthropic provider
// at request time like TS getHeaders/loadApiKey: the API key comes from
// Config.APIKey or the ANTHROPIC_API_KEY environment variable, and a missing
// key is a LoadAPIKeyError. Wrapper providers (UserAgentName/NoUserAgentTag
// set), AuthToken users and callers that authenticate through explicit
// Config.Headers (x-api-key or Authorization) keep their own credentials.
func requestAuthHeaders(cfg Config) (map[string]string, error) {
	if cfg.AuthToken != "" {
		return nil, nil
	}
	envVar, description := "ANTHROPIC_API_KEY", "Anthropic"
	if cfg.UserAgentName != "" || cfg.NoUserAgentTag {
		if cfg.APIKeyEnvVar == "" {
			return nil, nil
		}
		envVar, description = cfg.APIKeyEnvVar, cfg.APIKeyDescription
	}
	// An explicit x-api-key or Authorization header keeps the caller's own
	// credential.
	return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, envVar, description, "x-api-key", "", "authorization")
}

// newID returns a fresh generated id (see Config.GenerateID).
func (p *Provider) newID() string {
	if p != nil && p.config.GenerateID != nil {
		return p.config.GenerateID()
	}
	return anthropicGenerateID()
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
	baseURL = strings.TrimSuffix(baseURL, "/")
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

// baseName is the provider name without a trailing ".messages" (TS strips it
// from providerName), the prefix of the batch, skills and decision provider
// ids.
func (p *Provider) baseName() string {
	return strings.TrimSuffix(p.Name(), ".messages")
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
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Anthropic doesn't provide image generation models
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	// Anthropic doesn't provide speech synthesis models
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Anthropic doesn't provide transcription models
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Anthropic doesn't provide reranking models
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
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

// DecisionModel returns an experimental decision model backed by the
// Anthropic Messages API language model. Mirrors TypeScript's
// AnthropicProvider.decisionModel: `provider.decisionModel = (modelId) =>
// new DecisionLanguageModel({ model: createChatModel(modelId), provider:
// providerName + '.decision' })`.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	model, err := p.LanguageModel(modelID)
	if err != nil {
		return nil, err
	}
	return ai.NewDecisionLanguageModel(ai.DecisionLanguageModelOptions{
		Model:    model,
		Provider: p.baseName() + ".decision",
	})
}

// EvaluationModel is the deprecated form of DecisionModel.
//
// Deprecated: Use DecisionModel instead.
func (p *Provider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	model, err := p.DecisionModel(modelID)
	if err != nil {
		return nil, err
	}
	return provider.AsEvaluationModel(model), nil
}
