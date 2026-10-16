package heygen

import (
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DefaultBaseURL is the default HeyGen API URL prefix.
const DefaultBaseURL = "https://api.heygen.com"

// VideoModelHeyGenVideo1 is the HeyGen video generation model.
const VideoModelHeyGenVideo1 = "heygen-video-1"

// Provider implements the provider.Provider interface for HeyGen.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the HeyGen provider.
type Config struct {
	// APIKey is the key sent in the x-api-key header. If empty, it is read
	// from the HEYGEN_API_KEY environment variable lazily on each request
	// (mirrors TypeScript's lazy loadApiKey).
	APIKey string `json:"-"`

	// BaseURL is the API URL prefix. Defaults to https://api.heygen.com. A
	// trailing slash is stripped.
	BaseURL string `json:"baseURL,omitempty"`

	// Headers contains additional headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// PollIntervalMillis overrides the interval between status checks used by
	// VideoModel.DoGenerate's synchronous start+poll wrapper. Defaults to
	// 5000ms. HeyGen's TS provider has no doGenerate of its own; this only
	// governs a direct DoGenerate call.
	PollIntervalMillis int `json:"pollIntervalMillis,omitempty"`

	// PollTimeoutMillis overrides the total polling timeout for
	// VideoModel.DoGenerate. Defaults to 600000ms (10 minutes).
	PollTimeoutMillis int `json:"pollTimeoutMillis,omitempty"`
}

// New creates a new HeyGen provider.
func New(cfg Config) *Provider {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	client := internalhttp.NewClient(internalhttp.Config{BaseURL: cfg.BaseURL})
	return &Provider{config: cfg, client: client}
}

// CreateHeyGen creates a new HeyGen provider. It mirrors the TypeScript SDK's
// createHeyGen export while New remains the idiomatic Go constructor.
func CreateHeyGen(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string { return "heygen" }

// resolveHeaders returns the headers for a call: the x-api-key (from
// Config.APIKey or HEYGEN_API_KEY, resolved per call) plus configured custom
// headers and the SDK user-agent suffix, then the per-request headers.
func (p *Provider) resolveHeaders(requestHeaders map[string]string) (map[string]string, error) {
	apiKey, err := providerutils.LoadAPIKey(p.config.APIKey, "HEYGEN_API_KEY", "HeyGen")
	if err != nil {
		return nil, err
	}
	headers := internalhttp.MergeHeaders(map[string]string{"x-api-key": apiKey}, p.config.Headers)
	headers = version.WithUserAgentSuffix(headers, version.ProviderUserAgent("heygen"))
	return internalhttp.MergeHeaders(headers, requestHeaders), nil
}

// LanguageModel returns an error: HeyGen does not support language models.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "languageModel")
}

// EmbeddingModel returns an error: HeyGen does not support embeddings.
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an error: HeyGen does not support image generation.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns an error: HeyGen does not support speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns an error: HeyGen does not support transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns an error: HeyGen does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// VideoModel returns a video generation model by ID.
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = VideoModelHeyGenVideo1
	}
	return NewVideoModel(p, modelID), nil
}

// Video returns a video generation model by ID. It mirrors the TypeScript
// SDK's heygen.video() alias.
func (p *Provider) Video(modelID string) (provider.VideoModelV3, error) {
	return p.VideoModel(modelID)
}

// Client returns the HTTP client used for API requests.
func (p *Provider) Client() *internalhttp.Client { return p.client }
