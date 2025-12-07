package cerebras

import (
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Provider implements the provider.Provider interface for Cerebras
// Cerebras is OpenAI-compatible, so we use the OpenAI implementation
type Provider struct {
	*openai.Provider
}

// Config contains configuration for the Cerebras provider
type Config struct {
	// APIKey is the Cerebras API key
	APIKey string

	// BaseURL is the base URL for the Cerebras API (optional)
	BaseURL string
}

// New creates a new Cerebras provider
// Cerebras uses OpenAI-compatible API
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.cerebras.ai/v1"
	}

	openaiProvider := openai.New(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: baseURL,
	})

	return &Provider{
		Provider: openaiProvider,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "cerebras"
}
