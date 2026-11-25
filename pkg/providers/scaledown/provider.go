package scaledown

import (
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DefaultBaseURL is the ScaleDown API base URL.
const DefaultBaseURL = "https://api.scaledown.xyz"

// apiKeyEnvVar is the environment variable read when Config.APIKey is empty.
const apiKeyEnvVar = "SCALEDOWN_API_KEY"

// Config configures the ScaleDown provider. Mirrors TS ScaleDownProviderSettings.
type Config struct {
	// APIKey is sent as x-api-key. Defaults to the SCALEDOWN_API_KEY
	// environment variable, read per request.
	APIKey string `json:"-"`
	// BaseURL defaults to https://api.scaledown.xyz; a trailing slash is
	// removed.
	BaseURL string `json:"baseURL,omitempty"`
	// Headers are added to every request (they override x-api-key).
	Headers map[string]string `json:"headers,omitempty"`
	// HTTPClient is the underlying HTTP client (TS fetch).
	HTTPClient *http.Client `json:"-"`
}

// Provider is the ScaleDown provider.
type Provider struct {
	config  Config
	baseURL string
	client  *internalhttp.Client
}

var _ provider.Provider = (*Provider)(nil)
var _ provider.DecisionModelProvider = (*Provider)(nil)

// New creates a ScaleDown provider. Mirrors TS createScaleDown.
func New(cfg Config) *Provider {
	cfg.BaseURL = providerutils.WithoutTrailingSlash(cfg.BaseURL)
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Provider{
		config:  cfg,
		baseURL: baseURL,
		client:  internalhttp.NewClient(internalhttp.Config{BaseURL: baseURL, HTTPClient: cfg.HTTPClient}),
	}
}

// CreateScaleDown is New under the TS factory name.
func CreateScaleDown(cfg Config) *Provider { return New(cfg) }

// Name returns "scaledown".
func (p *Provider) Name() string { return "scaledown" }

// resolveHeaders is the TS provider headers function: {x-api-key: <key>,
// ...options.headers} with the ai-sdk-scaledown user-agent suffix. The key is
// loaded per call, so a missing key surfaces when a decision is made.
func (p *Provider) resolveHeaders() (map[string]string, error) {
	apiKey, err := providerutils.LoadAPIKey(p.config.APIKey, apiKeyEnvVar, "ScaleDown")
	if err != nil {
		return nil, err
	}
	headers := internalhttp.MergeHeaders(map[string]string{"x-api-key": apiKey}, p.config.Headers)
	return version.WithUserAgentSuffix(headers, version.ProviderUserAgent("scaledown")), nil
}

// DecisionModel returns a ScaleDown decision model (classify, classify-1 or a
// custom ID). Mirrors TS decisionModel.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	return NewDecisionModel(p, modelID), nil
}

// LanguageModel is not supported.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "languageModel")
}

// EmbeddingModel is not supported.
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel is not supported.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel is not supported.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel is not supported.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel is not supported.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}
