package fireworks

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Fireworks AI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Fireworks AI provider
type Config struct {
	// APIKey is the Fireworks AI API key
	APIKey string

	// BaseURL is the base URL for the Fireworks AI API (optional)
	BaseURL string

	// ImagePollIntervalMs is the interval between poll attempts for async image
	// generation models (e.g. flux-kontext-*). Defaults to 500ms.
	ImagePollIntervalMs int

	// ImagePollTimeoutMs is the maximum duration to wait for async image generation
	// to complete. Defaults to 120000ms (2 minutes).
	ImagePollTimeoutMs int

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Fireworks AI provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.fireworks.ai/inference"
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: http.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
			"Content-Type":  "application/json",
		}, cfg.Headers),
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateFireworks creates a new Fireworks AI provider.
//
// It mirrors the TypeScript SDK createFireworks export while New remains the
// idiomatic Go constructor.
func CreateFireworks(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "fireworks"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "accounts/fireworks/models/mixtral-8x7b-instruct"
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		modelID = "nomic-ai/nomic-embed-text-v1.5"
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "accounts/fireworks/models/stable-diffusion-xl-1024-v1-0"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("LFireworks AI does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("LFireworks AI does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("LFireworks AI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
