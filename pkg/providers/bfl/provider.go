package bfl

import (
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Black Forest Labs (FLUX)
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Black Forest Labs provider
type Config struct {
	// APIKey is the Black Forest Labs API key
	APIKey string

	// BaseURL is the base URL for the BFL API (optional)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// PollIntervalMillis is the default interval between image status checks.
	// Defaults to 500ms, matching the TypeScript provider.
	PollIntervalMillis int

	// PollTimeoutMillis is the default total polling timeout.
	// Defaults to 60000ms, matching the TypeScript provider.
	PollTimeoutMillis int
}

// New creates a new Black Forest Labs provider with the given configuration
func New(cfg Config) *Provider {
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("BFL_API_KEY")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: http.MergeHeaders(map[string]string{
			"X-Key":        cfg.APIKey,
			"Content-Type": "application/json",
		}, cfg.Headers),
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

func (p *Provider) baseURL() string {
	if p.config.BaseURL != "" {
		return p.config.BaseURL
	}
	return defaultBaseURL
}

const defaultBaseURL = "https://api.bfl.ai/v1"

// Name returns the provider name
func (p *Provider) Name() string {
	return "bfl"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("black forest labs does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("black forest labs does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "flux-pro"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("black forest labs does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("black forest labs does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("black forest labs does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
