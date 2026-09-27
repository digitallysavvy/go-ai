// Package cartesia provides a Cartesia speech and transcription provider for
// the Go AI SDK. Cartesia offers text-to-speech (POST /tts/bytes) and batch
// speech-to-text (POST /stt) endpoints.
//
// Ink 2 realtime/streaming transcription (WebSocket-based, over
// /stt/websocket and /stt/turns/websocket) is out of scope for this
// batch-only TranscriptionModel; it is designed to grow a DoStream method
// later without changing this package's exported API. DoTranscribe rejects
// streaming-only model IDs (ink-2 and ink-2-*).
package cartesia

import (
	"fmt"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// DefaultAPIVersion is the Cartesia API version sent via the
// Cartesia-Version header. See
// https://docs.cartesia.ai/api-reference/api-versioning
const DefaultAPIVersion = "2026-03-01"

// Config holds configuration for the Cartesia provider.
type Config struct {
	// APIKey is the Cartesia API key. Falls back to CARTESIA_API_KEY.
	APIKey string

	// BaseURL is the base URL for the Cartesia API (optional).
	BaseURL string

	// APIVersion is the Cartesia API version sent via the Cartesia-Version
	// header (optional, defaults to DefaultAPIVersion).
	APIVersion string
}

// Provider represents the Cartesia provider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a new Cartesia provider instance.
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.cartesia.ai"
	}
	if config.APIVersion == "" {
		config.APIVersion = DefaultAPIVersion
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: config.BaseURL,
		Headers: map[string]string{
			"Authorization":    "Bearer " + config.APIKey,
			"Cartesia-Version": config.APIVersion,
		},
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
	return "cartesia"
}

// LanguageModel returns a language model (not supported by Cartesia).
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("cartesia does not provide language models")
}

// EmbeddingModel returns an embedding model (not supported by Cartesia).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("cartesia does not provide embedding models")
}

// ImageModel returns an image model (not supported by Cartesia).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("cartesia does not provide image models")
}

// SpeechModel returns a text-to-speech model.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return &SpeechModel{provider: p, modelID: modelID}, nil
}

// TranscriptionModel returns a speech-to-text model.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return &TranscriptionModel{provider: p, modelID: modelID}, nil
}

// RerankingModel returns a reranking model (not supported by Cartesia).
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("cartesia does not provide reranking models")
}
