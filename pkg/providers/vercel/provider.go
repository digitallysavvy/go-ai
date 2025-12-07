package vercel

import (
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Provider implements the provider.Provider interface for Vercel AI
// Vercel AI is OpenAI-compatible, so we use the OpenAI implementation
type Provider struct {
	*openai.Provider
}

// Config contains configuration for the Vercel AI provider
type Config struct {
	// APIKey is the Vercel AI API key
	APIKey string

	// BaseURL is the base URL for the Vercel AI API (optional)
	BaseURL string
}

// New creates a new Vercel AI provider
// Vercel AI uses OpenAI-compatible API
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.vercel.com/v1"
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
	return "vercel"
}
