package moonshot

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default Moonshot API base URL
	DefaultBaseURL = "https://api.moonshot.cn/v1"
)

// Provider implements the provider.Provider interface for Moonshot AI
type Provider struct {
	config Config
	client *http.Client
}

// New creates a new Moonshot AI provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// Create HTTP client with authorization header
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
	return "moonshot"
}

// LanguageModel returns a language model by ID
// Supported models: moonshot-v1-8k, moonshot-v1-32k, moonshot-v1-128k,
// kimi-k2, kimi-k2.5, kimi-k2-thinking, kimi-k2-thinking-turbo, kimi-k2-turbo
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "moonshot-v1-32k" // Default model
	}

	// Validate model ID
	validModels := map[string]bool{
		"moonshot-v1-8k":            true,
		"moonshot-v1-32k":           true,
		"moonshot-v1-128k":          true,
		"kimi-k2":                   true,
		"kimi-k2-0905":              true,
		"kimi-k2-thinking":          true,
		"kimi-k2-thinking-turbo":    true,
		"kimi-k2-turbo":             true,
		"kimi-k2.5":                 true,
	}

	if !validModels[modelID] {
		return nil, fmt.Errorf("unsupported Moonshot model: %s", modelID)
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Moonshot provider does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("Moonshot provider does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Moonshot provider does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Moonshot provider does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Moonshot provider does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
