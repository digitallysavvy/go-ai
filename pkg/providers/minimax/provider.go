// Package minimax implements the MiniMax provider's chat/language-model
// surface. It mirrors @ai-sdk/minimax at ai@7.0.113 (video model support is
// added separately once the async video core lands).
//
// MiniMax chat delegates to the Anthropic Messages protocol: TS
// createMiniMax constructs an AnthropicLanguageModel directly against
// MiniMax's Anthropic-compatible endpoint, with config.provider =
// "minimax.messages". This Go package does the same by constructing a single
// shared *anthropic.LanguageModel (Provider.Name() = "minimax") and
// delegating every call to it: pkg/providers/anthropic's per-call
// provider-options resolution (call_options.go) already derives the
// "minimax" providerOptionsName and reads providerOptions.minimax /
// providerOptions.anthropic fresh on every call, so this package adds no
// MiniMax-specific request-building logic of its own.
package minimax

import (
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// DefaultBaseURL is MiniMax's Anthropic-compatible chat endpoint.
const DefaultBaseURL = "https://api.minimax.io/anthropic/v1"

// Provider implements the provider.Provider interface for MiniMax chat
// models.
type Provider struct {
	config            Config
	anthropicProvider *anthropic.Provider
}

// Config contains configuration for the MiniMax provider.
type Config struct {
	// APIKey is the MiniMax API key. Defaults to the MINIMAX_API_KEY
	// environment variable.
	APIKey string

	// BaseURL is the base URL for chat API calls. Defaults to MiniMax's
	// Anthropic-compatible endpoint (https://api.minimax.io/anthropic/v1).
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string
}

// New creates a new MiniMax provider with the given configuration. If
// Config.APIKey is empty, the API key is loaded from the MINIMAX_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("MINIMAX_API_KEY")
	}

	anthropicProvider := anthropic.New(anthropic.Config{
		APIKey:  apiKey,
		Name:    "minimax",
		BaseURL: baseURL,
		Headers: cfg.Headers,
		// TS sets supportedUrls: () => ({}) for MiniMax: unlike direct
		// Anthropic, it never accepts https image/PDF URLs directly and
		// always converts to base64.
		SupportedURLs: func(string) map[string][]string {
			return map[string][]string{}
		},
	})

	return &Provider{
		config:            cfg,
		anthropicProvider: anthropicProvider,
	}
}

// CreateMiniMax creates a new MiniMax provider.
//
// It mirrors the TypeScript SDK createMiniMax export while New remains the
// idiomatic Go constructor.
func CreateMiniMax(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "minimax"
}

// LanguageModel returns a MiniMax chat model by ID.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p, modelID), nil
}

// ChatModel is an alias for LanguageModel (TS provider.chat).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an error: MiniMax does not support embeddings (TS
// createMiniMax throws NoSuchModelError for embeddingModel).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("minimax provider does not support embedding models (no such model: %q)", modelID)
}

// ImageModel returns an error: MiniMax does not support image generation (TS
// createMiniMax throws NoSuchModelError for imageModel).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("minimax provider does not support image models (no such model: %q)", modelID)
}

// SpeechModel returns an error: MiniMax's chat surface does not support
// speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("minimax provider does not support speech synthesis")
}

// TranscriptionModel returns an error: MiniMax's chat surface does not
// support transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("minimax provider does not support transcription")
}

// RerankingModel returns an error: MiniMax does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("minimax provider does not support reranking")
}
