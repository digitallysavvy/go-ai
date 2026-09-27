package klingai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const defaultBaseURL = "https://api-singapore.klingai.com"

// Provider implements the provider.Provider interface for KlingAI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the KlingAI provider
type Config struct {
	// APIKey is the KlingAI API key, sent directly as a bearer token.
	// If empty, will use the KLINGAI_API_KEY environment variable.
	//
	// This is the recommended way to authenticate. When set (explicitly or
	// via the environment variable), it takes precedence over the legacy
	// AccessKey/SecretKey pair.
	APIKey string

	// AccessKey is the KlingAI access key (legacy authentication, used with
	// SecretKey to sign a short-lived JWT). Prefer APIKey instead.
	// If empty, will use KLINGAI_ACCESS_KEY environment variable
	AccessKey string

	// SecretKey is the KlingAI secret key (legacy authentication, used with
	// AccessKey to sign a short-lived JWT). Prefer APIKey instead.
	// If empty, will use KLINGAI_SECRET_KEY environment variable
	SecretKey string

	// BaseURL is the base URL for the KlingAI API (optional)
	// Defaults to https://api-singapore.klingai.com
	BaseURL string

	// Headers are additional headers to include in requests
	Headers map[string]string
}

// New creates a new KlingAI provider with the given configuration. Credential
// resolution (see resolveKlingAIAuthToken) happens lazily on every request
// via GenerateAuthToken, but New fails fast when no credential source is
// configured or available in the environment at all.
func New(cfg Config) (*Provider, error) {
	if _, err := resolveKlingAIAuthToken(cfg.APIKey, cfg.AccessKey, cfg.SecretKey); err != nil {
		return nil, err
	}

	// Use default base URL if not provided
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cfg.BaseURL = baseURL

	// Create HTTP client (auth header will be added per-request)
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: cfg.Headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}, nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "klingai"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("KlingAI does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("KlingAI does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("KlingAI does not support image generation")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required")
	}

	return newVideoModel(p, modelID)
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("KlingAI does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("KlingAI does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("KlingAI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// GenerateAuthToken resolves the bearer token for the next KlingAI API
// request: the configured API key when present, otherwise a freshly signed
// JWT from the legacy access/secret key pair (see resolveKlingAIAuthToken).
func (p *Provider) GenerateAuthToken() (string, error) {
	return resolveKlingAIAuthToken(p.config.APIKey, p.config.AccessKey, p.config.SecretKey)
}
