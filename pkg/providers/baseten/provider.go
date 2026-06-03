package baseten

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Provider implements the provider.Provider interface for Baseten
// Baseten is OpenAI-compatible, so we use the OpenAI implementation
type Provider struct {
	*openai.Provider
}

// Config contains configuration for the Baseten provider
type Config struct {
	// APIKey is the Baseten API key
	APIKey string

	// BaseURL is the base URL for the Baseten API (optional)
	BaseURL string
}

// New creates a new Baseten provider
// Baseten uses OpenAI-compatible API
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://bridge.baseten.co/v1"
	}

	openaiProvider := openai.New(openai.Config{
		APIKey:           cfg.APIKey,
		BaseURL:          baseURL,
		ChatProviderName: "openai",
	})

	return &Provider{
		Provider: openaiProvider,
	}
}

// LanguageModel returns an OpenAI-compatible Chat Completions model.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.Provider.ChatModel(modelID)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "baseten"
}
