package fal

import (
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// defaultBaseURL matches the TS SDK's `defaultBaseURL = 'https://fal.run'`
// (fal-provider.ts). Model IDs are used verbatim as the path suffix (e.g.
// "fal-ai/kling-video/..."), so the base URL must NOT include a "/fal-ai"
// segment or every fully-qualified model ID would be double-prefixed.
const defaultBaseURL = "https://fal.run"

// falRunHost and falQueueHost are the fixed hosts the TS SDK's
// FalSpeechModel/FalTranscriptionModel/FalVideoModel hit directly
// (`url: ({ path }) => path`), independent of the provider's configured
// BaseURL (which only applies to FalImageModel). Kept as Provider fields
// (defaulted here) so tests can redirect them to an httptest server.
const (
	falRunHost   = "https://fal.run"
	falQueueHost = "https://queue.fal.run"
)

// Provider implements the provider.Provider interface for Fal.ai
type Provider struct {
	config Config
	client *http.Client

	// absClient issues requests against fully-qualified URLs (baseURL "").
	// Used by the speech and transcription models, which hit falRunHost/
	// falQueueHost directly rather than the configurable client baseURL.
	absClient *http.Client

	// speechHost/queueHost default to falRunHost/falQueueHost; overridable
	// (in-package tests only) to redirect speech/transcription requests to
	// an httptest server.
	speechHost string
	queueHost  string
}

// Config contains configuration for the Fal.ai provider
type Config struct {
	// APIKey is the Fal.ai API key. Falls back to the FAL_API_KEY
	// environment variable, then to FAL_KEY, matching the TS SDK's
	// loadFalApiKey (fal-provider.ts).
	APIKey string

	// BaseURL is the base URL for the Fal.ai API (optional)
	BaseURL string

	// Headers are custom HTTP headers to include in every request, matching
	// the TS SDK's FalProviderSettings.headers (fal-provider.ts). Applied to
	// all model types (image, video, speech, transcription), mirroring the
	// TS provider's single getHeaders() used by every model factory.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Fal.ai provider with the given configuration
func New(cfg Config) *Provider {
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("FAL_API_KEY")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("FAL_KEY")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	headers := version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
		"Authorization": "Key " + cfg.APIKey,
		"Content-Type":  "application/json",
	}, cfg.Headers), version.ProviderUserAgent("fal"))

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	absClient := http.NewClient(http.Config{
		BaseURL: "",
		Headers: headers,
	})

	return &Provider{
		config:     cfg,
		client:     client,
		absClient:  absClient,
		speechHost: falRunHost,
		queueHost:  falQueueHost,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "fal"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("fal.ai does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("fal.ai does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "fal-ai/fast-sdxl"
	}

	return NewImageModel(p, modelID), nil
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = "fal-ai/luma-ray" // Default FAL video model
	}

	return NewVideoModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return NewSpeechModel(p, modelID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Fal.ai does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
