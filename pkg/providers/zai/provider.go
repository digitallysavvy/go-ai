// Package zai implements the Z.AI provider (GLM chat models over an
// OpenAI-compatible API). It mirrors @ai-sdk/zai at ai@7.0.113.
package zai

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DefaultBaseURL is the default Z.AI API base URL.
const DefaultBaseURL = "https://api.z.ai/api/paas/v4"

// Provider implements the provider.Provider interface for Z.AI.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the Z.AI provider.
type Config struct {
	// APIKey is the Z.AI API key. Defaults to the ZAI_API_KEY environment
	// variable.
	APIKey string

	// BaseURL is the base URL for API calls. Defaults to
	// https://api.z.ai/api/paas/v4.
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string
}

// New creates a new Z.AI provider with the given configuration. If
// Config.APIKey is empty, the API key is loaded from the ZAI_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ZAI_API_KEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("zai")),
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateZai creates a new Z.AI provider.
//
// It mirrors the TypeScript SDK createZai export while New remains the
// idiomatic Go constructor.
func CreateZai(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "zai"
}

// LanguageModel returns a Z.AI chat model by ID.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p, modelID), nil
}

// ChatModel is an alias for LanguageModel (TS provider.chat).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an error: Z.AI does not support embeddings (TS
// createZai throws NoSuchModelError for embeddingModel).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("zai provider does not support embedding models (no such model: %q)", modelID)
}

// ImageModel returns an error: Z.AI does not support image generation (TS
// createZai throws NoSuchModelError for imageModel).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("zai provider does not support image models (no such model: %q)", modelID)
}

// SpeechModel returns an error: Z.AI does not support speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("zai provider does not support speech synthesis")
}

// TranscriptionModel returns an error: Z.AI does not support transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("zai provider does not support transcription")
}

// RerankingModel returns an error: Z.AI does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("zai provider does not support reranking")
}

// Client returns the HTTP client used for API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}
