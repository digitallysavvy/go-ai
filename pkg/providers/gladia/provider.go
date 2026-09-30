// Package gladia provides a Gladia AI speech-to-text provider for the Go AI SDK.
// Gladia offers advanced speech recognition with async processing and multi-language support.
package gladia

import (
	"fmt"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Config holds configuration for the Gladia provider
type Config struct {
	// API key for authentication
	APIKey string

	// Base URL for the Gladia API (optional, defaults to production)
	BaseURL string
}

// Provider represents the Gladia AI provider
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a new Gladia provider instance
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.gladia.io/v2"
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: config.BaseURL,
		Headers: version.WithUserAgentSuffix(map[string]string{
			"x-gladia-key": config.APIKey,
		}, version.ProviderUserAgent("gladia")),
	})

	return &Provider{
		config: config,
		client: client,
	}
}

// Client returns the HTTP client for making API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "gladia"
}

// LanguageModel returns a language model (not supported by Gladia)
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("gladia does not provide language models")
}

// EmbeddingModel returns an embedding model (not supported by Gladia)
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("gladia does not provide embedding models")
}

// ImageModel returns an image model (not supported by Gladia)
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("gladia does not provide image models")
}

// SpeechModel returns a speech synthesis model (not supported by Gladia)
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("gladia does not provide speech synthesis models")
}

// TranscriptionModel returns a speech-to-text model
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return &TranscriptionModel{
		provider: p,
		modelID:  modelID,
	}, nil
}

// RerankingModel returns a reranking model (not supported by Gladia)
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("gladia does not provide reranking models")
}
