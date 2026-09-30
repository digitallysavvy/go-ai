package deepgram

import (
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Deepgram
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Deepgram provider
type Config struct {
	// APIKey is the Deepgram API key. Falls back to DEEPGRAM_API_KEY.
	APIKey string

	// BaseURL is the base URL for the Deepgram API (optional)
	BaseURL string
}

// New creates a new Deepgram provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepgram.com"
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("DEEPGRAM_API_KEY")
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(map[string]string{
			"Authorization": "Token " + cfg.APIKey,
			"Content-Type":  "application/json",
		}, version.ProviderUserAgent("deepgram")),
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "deepgram"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("deepgram does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("deepgram does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("deepgram does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return NewSpeechModel(p, modelID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		modelID = "nova-2"
	}

	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("deepgram does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
