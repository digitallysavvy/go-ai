package deepinfra

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Provider implements the provider.Provider interface for DeepInfra
// DeepInfra is OpenAI-compatible, so we use the OpenAI implementation
// with custom fixes for token counting issues
type Provider struct {
	*openai.Provider
}

// Config contains configuration for the DeepInfra provider
type Config struct {
	// APIKey is the DeepInfra API key
	APIKey string

	// BaseURL is the base URL for the DeepInfra API (optional)
	BaseURL string
}

// New creates a new DeepInfra provider
// DeepInfra uses OpenAI-compatible API
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepinfra.com/v1/openai"
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
	return "deepinfra"
}

// LanguageModel returns a language model with DeepInfra-specific fixes
// This includes token counting corrections for Gemini/Gemma models
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p.Provider, modelID), nil
}
