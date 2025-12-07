package anthropic

import (
	"fmt"

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

	// BaseURL is the base URL for the Anthropic API (default: https://api.anthropic.com)
	BaseURL string

	// APIVersion is the Anthropic API version (default: 2023-06-01)
	APIVersion string
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
	headers := map[string]string{
		"x-api-key":         cfg.APIKey,
		"anthropic-version": apiVersion,
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
	return "anthropic"
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
	// Anthropic doesn't provide embedding models
	return nil, fmt.Errorf("Anthropic does not support embedding models")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Anthropic doesn't provide image generation models
	return nil, fmt.Errorf("Anthropic does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	// Anthropic doesn't provide speech synthesis models
	return nil, fmt.Errorf("Anthropic does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Anthropic doesn't provide transcription models
	return nil, fmt.Errorf("Anthropic does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Anthropic doesn't provide reranking models
	return nil, fmt.Errorf("Anthropic does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
