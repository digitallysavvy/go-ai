package perplexity

import (
	"context"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const (
	DefaultBaseURL = "https://api.perplexity.ai"
	DefaultModelID = string(ModelSonar)
)

// Provider implements the provider.Provider interface for Perplexity
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Perplexity provider
type Config struct {
	// APIKey is the Perplexity API key
	APIKey string

	// APIKeyFunc resolves the API key before each request (TS apiKey given as
	// a function, 7716e189ee), for keys that are looked up, refreshed or
	// rotated. It cannot be combined with APIKey; the environment variable is
	// not read, and its result is sent as is.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

	// BaseURL is the base URL for the Perplexity API (optional)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Perplexity provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	cfg.BaseURL = baseURL
	apiKey := cfg.APIKey
	if apiKey == "" && cfg.APIKeyFunc == nil {
		apiKey = os.Getenv("PERPLEXITY_API_KEY")
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
			"Authorization":      "Bearer " + apiKey,
			"Content-Type":       "application/json",
			"X-Pplx-Integration": "vercel-ai-sdk",
		}, cfg.Headers), version.ProviderUserAgent("perplexity")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			if cfg.APIKeyFunc != nil {
				// HeadersFunc has no request ctx (reports/cr-r85-prova-to-carry.md).
				return providerutils.RequestAuthHeaderFunc(context.Background(), cfg.Headers, providerutils.ResolveAPIKeyOptions{
					APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, EnvVar: "PERPLEXITY_API_KEY", Description: "Perplexity",
				}, "Authorization", "Bearer ")
			}
			return providerutils.RequestAuthHeader(cfg.Headers, apiKey, "PERPLEXITY_API_KEY", "Perplexity", "Authorization", "Bearer ")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreatePerplexity creates a new Perplexity provider.
//
// It mirrors the TypeScript SDK createPerplexity export while New remains the
// idiomatic Go constructor.
func CreatePerplexity(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "perplexity"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = DefaultModelID
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		modelID = string(ModelEmbedV1_4B)
	}
	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// DecisionModel returns an experimental decision model backed by the
// Perplexity Decisions API. Mirrors TS PerplexityProvider.decisionModel.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	return NewDecisionModel(p, modelID), nil
}
