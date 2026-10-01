package stability

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
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
		Headers: version.WithUserAgentSuffix(map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
			"Content-Type":  "application/json",
		}, version.ProviderUserAgent("stability")),
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
	return nil, fmt.Errorf("Stability AI does not support language models") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Stability AI does not support embeddings") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
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
	return nil, fmt.Errorf("Stability AI does not support speech synthesis") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Stability AI does not support transcription") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Stability AI does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
