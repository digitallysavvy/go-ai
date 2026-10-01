// Package fishaudio provides a Fish Audio speech and transcription provider
// for the Go AI SDK. Fish Audio offers text-to-speech (POST /v1/tts) and
// speech-to-text (POST /v1/asr) endpoints.
package fishaudio

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Config holds configuration for the Fish Audio provider.
type Config struct {
	// APIKey is the Fish Audio API key. Falls back to FISH_AUDIO_API_KEY.
	APIKey string

	// BaseURL is the base URL for the Fish Audio API (optional).
	BaseURL string
}

// Provider represents the Fish Audio provider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a new Fish Audio provider instance.
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.fish.audio"
	}
	if config.APIKey == "" {
		config.APIKey = os.Getenv("FISH_AUDIO_API_KEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: config.BaseURL,
		Headers: version.WithUserAgentSuffix(map[string]string{
			"Authorization": "Bearer " + config.APIKey,
		}, version.ProviderUserAgent("fish-audio")),
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
	return "fish-audio"
}

// LanguageModel returns a language model (not supported by Fish Audio).
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("fish-audio does not provide language models")
}

// EmbeddingModel returns an embedding model (not supported by Fish Audio).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("fish-audio does not provide embedding models")
}

// ImageModel returns an image model (not supported by Fish Audio).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("fish-audio does not provide image models")
}

// SpeechModel returns a text-to-speech model.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return &SpeechModel{provider: p, modelID: modelID}, nil
}

// TranscriptionModel returns a speech-to-text model. `/v1/asr` has no model
// selector, so the model ID is a routing label and defaults to
// "transcribe-1", matching the TypeScript provider.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		modelID = ModelTranscribe1
	}
	return &TranscriptionModel{provider: p, modelID: modelID}, nil
}

// RerankingModel returns a reranking model (not supported by Fish Audio).
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("fish-audio does not provide reranking models")
}
