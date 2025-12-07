package perplexity

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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

	// BaseURL is the base URL for the Perplexity API (optional)
	BaseURL string
}

// New creates a new Perplexity provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.perplexity.ai"
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
			"Content-Type":  "application/json",
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "perplexity"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "llama-3.1-sonar-small-128k-online"
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Perplexity does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("Perplexity does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Perplexity does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Perplexity does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Perplexity does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
