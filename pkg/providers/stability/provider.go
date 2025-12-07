package stability

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Stability AI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Stability AI provider
type Config struct {
	// APIKey is the Stability AI API key
	APIKey string

	// BaseURL is the base URL for the Stability AI API (optional)
	BaseURL string
}

// New creates a new Stability AI provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.stability.ai"
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
	return "stability"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("Stability AI does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Stability AI does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "stable-diffusion-xl-1024-v1-0"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Stability AI does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Stability AI does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Stability AI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
