package cohere

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default Cohere API base URL
	DefaultBaseURL = "https://api.cohere.ai/v1"
)

// Provider implements the provider.Provider interface for Cohere
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Cohere provider
type Config struct {
	// APIKey is the Cohere API key
	APIKey string

	// BaseURL is the base URL for the Cohere API (default: https://api.cohere.ai/v1)
	BaseURL string
}

// New creates a new Cohere provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// Create HTTP client with default headers
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", cfg.APIKey),
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "cohere"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("Cohere does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Cohere does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Cohere does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewRerankingModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
