// Package topaz implements the Go AI SDK provider for Topaz Labs
// (https://www.topazlabs.com), mirroring the TypeScript @ai-sdk/topaz
// package.
//
// Topaz models enhance media the caller supplies (upscale, denoise, sharpen,
// restore) and do not generate from a text prompt. The image model
// (Wonder 3.5) submits an async enhance job, polls it, and downloads the
// result. The video model implements the spec-v4 async operation protocol
// (DoStart/DoStatus) on Topaz's express endpoint. Topaz has no language,
// embedding, speech, transcription, or reranking model.
package topaz

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const defaultBaseURL = "https://api.topazlabs.com"

// Provider implements the provider.Provider interface for Topaz Labs.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the Topaz Labs provider.
type Config struct {
	// APIKey is the Topaz Labs API key.
	// If empty, the TOPAZ_API_KEY environment variable is used.
	APIKey string

	// BaseURL is the base URL for the Topaz API (optional).
	// Defaults to https://api.topazlabs.com
	BaseURL string

	// Headers contains custom headers to include in all requests.
	Headers map[string]string `json:"headers,omitempty"`

	// VideoPollIntervalMillis overrides the interval between status checks
	// used by VideoModel.DoGenerate's synchronous start+poll wrapper.
	// Defaults to 2000ms. Topaz's TS provider has no doGenerate of its own
	// (the TS SDK core polls doStart/doStatus directly, which is what
	// pkg/ai's GenerateVideo uses here too); this only governs a direct
	// DoGenerate call.
	VideoPollIntervalMillis int `json:"videoPollIntervalMillis,omitempty"`

	// VideoPollTimeoutMillis overrides the total polling timeout for
	// VideoModel.DoGenerate's synchronous start+poll wrapper. Defaults to
	// 600000ms (10 minutes).
	VideoPollTimeoutMillis int `json:"videoPollTimeoutMillis,omitempty"`
}

// New creates a new Topaz Labs provider with the given configuration.
// If Config.APIKey is empty, the API key is loaded from the TOPAZ_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cfg.BaseURL = baseURL

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("TOPAZ_API_KEY")
	}
	cfg.APIKey = apiKey

	headers := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
		"X-API-Key": apiKey,
		"Accept":    "application/json",
	}, cfg.Headers), version.ProviderUserAgent("topaz"))

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateTopaz creates a new Topaz Labs provider.
//
// It mirrors the TypeScript SDK's createTopaz export while New remains the
// idiomatic Go constructor.
func CreateTopaz(cfg Config) *Provider {
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
	return "topaz"
}

// LanguageModel returns an error: Topaz does not support language models.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("topaz does not support language models")
}

// EmbeddingModel returns an error: Topaz does not support embeddings.
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("topaz does not support embeddings")
}

// ImageModel returns an image enhancement model by ID.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = ImageModelWonder35
	}
	return NewImageModel(p, modelID), nil
}

// Image returns an image enhancement model by ID.
// It mirrors the TypeScript SDK's topaz.image() alias.
func (p *Provider) Image(modelID string) (provider.ImageModel, error) {
	return p.ImageModel(modelID)
}

// VideoModel returns a video enhancement model by ID.
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = VideoModelProteus
	}
	return NewVideoModel(p, modelID), nil
}

// Video returns a video enhancement model by ID.
// It mirrors the TypeScript SDK's topaz.video() alias.
func (p *Provider) Video(modelID string) (provider.VideoModelV3, error) {
	return p.VideoModel(modelID)
}

// SpeechModel returns an error: Topaz does not support speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("topaz does not support speech synthesis")
}

// TranscriptionModel returns an error: Topaz does not support transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("topaz does not support transcription")
}

// RerankingModel returns an error: Topaz does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("topaz does not support reranking")
}

// Client returns the HTTP client for making API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}
