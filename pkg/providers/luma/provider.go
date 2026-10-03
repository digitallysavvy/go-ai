// Package luma implements the Go AI SDK provider for Luma AI
// (https://lumalabs.ai), mirroring the TypeScript @ai-sdk/luma package.
//
// Luma exposes an asynchronous image generation queue: DoGenerate submits a
// generation, polls its status, and downloads the resulting image. Luma has
// no language, embedding, speech, transcription, reranking, or video model.

package luma

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const defaultBaseURL = "https://api.lumalabs.ai"

// Provider implements the provider.Provider interface for Luma AI.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the Luma provider.
type Config struct {
	// APIKey is the Luma API key.
	// If empty, the LUMA_API_KEY environment variable is used.
	APIKey string

	// BaseURL is the base URL for the Luma API (optional).
	// Defaults to https://api.lumalabs.ai
	BaseURL string

	// Headers contains custom headers to include in all requests.
	Headers map[string]string
}

// New creates a new Luma provider with the given configuration.
// If Config.APIKey is empty, the API key is loaded from the LUMA_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cfg.BaseURL = baseURL

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("LUMA_API_KEY")
	}
	cfg.APIKey = apiKey

	headers := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, cfg.Headers), version.ProviderUserAgent("luma"))

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateLuma creates a new Luma provider.
//
// It mirrors the TypeScript SDK's createLuma export while New remains the
// idiomatic Go constructor.
func CreateLuma(cfg Config) *Provider {
	return New(cfg)
}

func (p *Provider) baseURL() string {
	if p.config.BaseURL != "" {
		return p.config.BaseURL
	}
	return defaultBaseURL
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "luma"
}

// LanguageModel returns an error: Luma does not support language models.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("luma does not support language models")
}

// EmbeddingModel returns an error: Luma does not support embeddings.
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("luma does not support embeddings")
}

// ImageModel returns an image generation model by ID.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = ModelPhoton1
	}
	return NewImageModel(p, modelID), nil
}

// Image returns an image generation model by ID.
// It mirrors the TypeScript SDK's luma.image() alias.
func (p *Provider) Image(modelID string) (provider.ImageModel, error) {
	return p.ImageModel(modelID)
}

// SpeechModel returns an error: Luma does not support speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("luma does not support speech synthesis")
}

// TranscriptionModel returns an error: Luma does not support transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("luma does not support transcription")
}

// RerankingModel returns an error: Luma does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("luma does not support reranking")
}

// Client returns the HTTP client for making API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}
