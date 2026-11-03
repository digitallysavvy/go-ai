package elevenlabs

import (
	"context"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for ElevenLabs
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the ElevenLabs provider
type Config struct {
	// APIKey is the ElevenLabs API key. Falls back to ELEVENLABS_API_KEY.
	APIKey string

	// APIKeyFunc resolves the API key before each request (TS apiKey given as
	// a function, 7716e189ee), for keys that are looked up, refreshed or
	// rotated. It cannot be combined with APIKey; the environment variable is
	// not read, and its result is sent as is.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

	// BaseURL is the base URL for the ElevenLabs API (optional)
	BaseURL string

	// Headers are custom HTTP headers sent with every request (TS provider
	// settings `headers`).
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new ElevenLabs provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.elevenlabs.io"
	}
	// Keep the resolved URL: the WebSocket models derive their endpoints from
	// config.BaseURL, which would otherwise stay empty with the default API.
	cfg.BaseURL = baseURL
	if cfg.APIKey == "" && cfg.APIKeyFunc == nil {
		cfg.APIKey = os.Getenv("ELEVENLABS_API_KEY")
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
			"xi-api-key":   cfg.APIKey,
			"Content-Type": "application/json",
		}, cfg.Headers), version.ProviderUserAgent("elevenlabs")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
			if cfg.APIKeyFunc != nil {
				return providerutils.RequestAuthHeaderFunc(ctx, cfg.Headers, providerutils.ResolveAPIKeyOptions{
					APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, EnvVar: "ELEVENLABS_API_KEY", Description: "ElevenLabs",
				}, "xi-api-key", "")
			}
			return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "ELEVENLABS_API_KEY", "ElevenLabs", "xi-api-key", "")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "elevenlabs"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "languageModel", Message: "ElevenLabs does not provide language models"}
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "embeddingModel", Message: "ElevenLabs does not provide embedding models"}
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "imageModel", Message: "ElevenLabs does not provide image models"}
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return NewSpeechModel(p, modelID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
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
