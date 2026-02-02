package google

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default Google Generative AI API base URL
	DefaultBaseURL = "https://generativelanguage.googleapis.com"
)

// Provider implements the provider.Provider interface for Google (Gemini)
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Google provider
type Config struct {
	// APIKey is the Google API key
	APIKey string

	// BaseURL is the base URL for the Google API (default: https://generativelanguage.googleapis.com)
	BaseURL string
}

// New creates a new Google provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// Google uses API key in query parameter, not header
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "google"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Google doesn't provide image generation through this API
	return nil, fmt.Errorf("Google does not support image generation through this API")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	// Google doesn't provide speech synthesis through this API
	return nil, fmt.Errorf("Google does not support speech synthesis through this API")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Google doesn't provide transcription through this API
	return nil, fmt.Errorf("Google does not support transcription through this API")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Google doesn't provide reranking models
	return nil, fmt.Errorf("Google does not support reranking")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewVideoModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// APIKey returns the API key
func (p *Provider) APIKey() string {
	return p.config.APIKey
}
