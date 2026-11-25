// Package parallel is the Parallel provider (TS @ai-sdk/parallel,
// 6a648d45ff): web-grounded typed decisions through the Parallel Decisions
// API. It exposes only a decision model; every other model family is a
// NoSuchModelError.
package parallel

import (
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultBaseURL is the Parallel API base URL.
const DefaultBaseURL = "https://api.parallel.ai/v1"

// apiKeyEnvVar is the environment variable read when Config.APIKey is empty.
const apiKeyEnvVar = "PARALLEL_API_KEY"

// DecisionModelFast is the "fast" decision mode. Any other mode string is
// forwarded as is (TS ParallelDecisionModelId = 'fast' | string).
const DecisionModelFast = "fast"

// Config configures the Parallel provider. Mirrors TS
// ParallelProviderSettings.
type Config struct {
	// APIKey is sent as x-api-key. Defaults to the PARALLEL_API_KEY
	// environment variable, read per request.
	APIKey string
	// BaseURL defaults to https://api.parallel.ai/v1; a trailing slash is
	// removed.
	BaseURL string
	// Headers are added to every request (they override x-api-key).
	Headers map[string]string `json:"headers,omitempty"`
	// HTTPClient is the underlying HTTP client (TS fetch).
	HTTPClient *http.Client `json:"-"`
}

// Provider is the Parallel provider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

var _ provider.Provider = (*Provider)(nil)
var _ provider.DecisionModelProvider = (*Provider)(nil)

// New creates a Parallel provider. Mirrors TS createParallel.
func New(cfg Config) *Provider {
	cfg.BaseURL = providerutils.WithoutTrailingSlash(cfg.BaseURL)
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Provider{
		config: cfg,
		client: internalhttp.NewClient(internalhttp.Config{BaseURL: baseURL, HTTPClient: cfg.HTTPClient}),
	}
}

// CreateParallel is New under the TS factory name.
func CreateParallel(cfg Config) *Provider { return New(cfg) }

// Name returns "parallel".
func (p *Provider) Name() string { return "parallel" }

// DecisionModel returns a web-grounded decision model for a mode such as
// "fast". An empty mode is a NoSuchModelError, as in TS.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	if modelID == "" {
		return nil, providererrors.NewNoSuchModelError(modelID, "decisionModel")
	}
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
