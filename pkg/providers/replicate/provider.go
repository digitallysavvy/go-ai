package replicate

import (
	"fmt"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Replicate
// Note: Replicate uses a polling-based API that requires:
// 1. Creating a prediction
// 2. Polling for completion
// 3. Retrieving results
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Replicate provider
type Config struct {
	// APIKey is the Replicate API token
	APIKey string

	// BaseURL is the base URL for the Replicate API (optional)
	BaseURL string

	// Headers are custom HTTP headers sent with every request (TS provider
	// settings `headers`).
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Replicate provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	// TS validateBaseURL(options.baseURL): a whitespace-only base URL is an
	// InvalidArgumentError. TS throws from the constructor; New keeps
	// returning a provider and every request fails with that error instead.
	baseURLErr := providerutils.ValidateBaseURL(baseURL)
	if baseURL == "" {
		baseURL = "https://api.replicate.com/v1"
	}

	// TS: apiToken ?? REPLICATE_API_TOKEN, loaded per request (loadApiKey).
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("REPLICATE_API_TOKEN")
	}
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("replicate")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			if baseURLErr != nil {
				return nil, baseURLErr
			}
			return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "REPLICATE_API_TOKEN", "Replicate", "Authorization", "Bearer ")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// baseURL returns the developer-configured API base URL (never derived from
// response data), defaulting like New.
func (p *Provider) baseURL() string {
	if p.config.BaseURL != "" {
		return p.config.BaseURL
	}
	return "https://api.replicate.com/v1"
}

// credentialedHeaders returns the provider headers merged with extra, but only
// when rawURL is same-origin with the configured base URL; otherwise nil.
// URLs taken from a response body (prediction urls.get) must never receive
// the API token (TS getFromApi credentialedOrigin: this.config.baseURL).
func (p *Provider) credentialedHeaders(rawURL string, extra map[string]string) map[string]string {
	if !fileutil.IsSameOrigin(rawURL, p.baseURL()) {
		return nil
	}
	out := map[string]string{}
	for k, v := range p.client.Headers() {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "replicate"
}

// LanguageModel returns a language model by model version or model:version string
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("replicate requires a model ID (e.g., 'meta/llama-2-70b-chat')")
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "stability-ai/sdxl:39ed52f2a78e934b3ba6e2a89f5b1c712de7dfea535525255b1aa35c5565e08b"
	}

	return NewImageModel(p, modelID), nil
}

// VideoModel returns a video generation model by ID
// modelID should be the full version string (e.g., "chenxwh/minimax-video-01-i2v:...")
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		return nil, fmt.Errorf("replicate requires a model version ID for video generation")
	}

	return NewVideoModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
