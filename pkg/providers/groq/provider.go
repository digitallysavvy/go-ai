package groq

import (
	"context"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Groq
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Groq provider
type Config struct {
	// APIKey is the Groq API key
	APIKey string

	// APIKeyFunc resolves the API key before each request (TS apiKey given as
	// a function, 7716e189ee), for keys that are looked up, refreshed or
	// rotated. It cannot be combined with APIKey; the environment variable is
	// not read, and its result is sent as is.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

	// BaseURL is the base URL for the Groq API (optional)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Groq provider with the given configuration
func New(cfg Config) *Provider {
	// TS loadApiKey: the key defaults to the GROQ_API_KEY environment variable.
	if cfg.APIKeyFunc == nil {
		cfg.APIKey = providerutils.APIKeyOrEnv(cfg.APIKey, "GROQ_API_KEY")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai"
	}
	// TS withoutTrailingSlash(baseURL).
	baseURL = providerutils.WithoutTrailingSlash(baseURL)
	// TS baseURL values include the version segment (".../v1"); every request
	// path here already starts with "/v1/", so accept both spellings.
	baseURL = strings.TrimSuffix(baseURL, "/v1")

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("groq")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
			if cfg.APIKeyFunc != nil {
				return providerutils.RequestAuthHeaderFunc(ctx, cfg.Headers, providerutils.ResolveAPIKeyOptions{
					APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, EnvVar: "GROQ_API_KEY", Description: "Groq",
				}, "Authorization", "Bearer ")
			}
			return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "GROQ_API_KEY", "Groq", "Authorization", "Bearer ")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateGroq creates a new Groq provider.
//
// It mirrors the TypeScript SDK createGroq export while New remains the
// idiomatic Go constructor.
func CreateGroq(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "groq"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "mixtral-8x7b-32768"
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID (Groq Whisper,
// POST /audio/transcriptions). Batch/non-streaming only.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		modelID = "whisper-large-v3-turbo"
	}
	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
