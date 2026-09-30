// Package hume provides a Hume speech synthesis provider for the Go AI SDK.
// Hume offers text-to-speech via POST /v0/tts/file. Hume has no selectable
// model ID (the SpeechModel's ModelID is always ""), matching the TypeScript
// SDK's HumeSpeechModel.
package hume

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Config holds configuration for the Hume provider.
type Config struct {
	// APIKey is the Hume API key. Falls back to HUME_API_KEY.
	APIKey string

	// BaseURL is the base URL for the Hume API (optional).
	BaseURL string
}

// Provider represents the Hume provider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a new Hume provider instance.
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.hume.ai"
	}
	if config.APIKey == "" {
		config.APIKey = os.Getenv("HUME_API_KEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: config.BaseURL,
		Headers: version.WithUserAgentSuffix(map[string]string{
			"X-Hume-Api-Key": config.APIKey,
		}, version.ProviderUserAgent("hume")),
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

// Name returns the provider name.
func (p *Provider) Name() string {
	return "hume"
}

// LanguageModel returns a language model (not supported by Hume).
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("hume does not provide language models")
}

// EmbeddingModel returns an embedding model (not supported by Hume).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("hume does not provide embedding models")
}

// ImageModel returns an image model (not supported by Hume).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("hume does not provide image models")
}

// SpeechModel returns a text-to-speech model. Hume has no selectable model
// ID; modelID is accepted for interface parity but ignored, matching the
// TypeScript SDK's `speech()` factory (no modelId parameter).
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return &SpeechModel{provider: p}, nil
}

// TranscriptionModel returns a speech-to-text model (not supported by Hume).
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("hume does not provide transcription models")
}

// RerankingModel returns a reranking model (not supported by Hume).
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("hume does not provide reranking models")
}
