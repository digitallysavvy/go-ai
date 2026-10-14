// Package typesafeai implements the TypeSafe AI provider (decision only).
// Mirrors TypeScript's @ai-sdk/typesafe-ai package: the provider exposes only
// the experimental DecisionModel capability; languageModel/embeddingModel/
// imageModel all report an unsupported-model error.
package typesafeai

import (
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DefaultBaseURL is the default TypeSafe AI API base URL.
const DefaultBaseURL = "https://api.typesafe.ai/v1"

// Provider implements provider.Provider (language/embedding/image factories
// all report unsupported) and provider.DecisionModelProvider for TypeSafe
// AI. Mirrors TypeScript's TypeSafeAiProvider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the TypeSafe AI provider.
type Config struct {
	// APIKey is the TypeSafe AI API key. If empty, it is read from the
	// TYPESAFE_AI_API_KEY environment variable at call time (mirrors
	// TypeScript's lazy loadApiKey behavior).
	APIKey string

	// BaseURL overrides the API base URL. Defaults to the TYPESAFE_AI_BASE_URL
	// environment variable, then DefaultBaseURL. A trailing slash is stripped.
	BaseURL string

	// Headers are additional HTTP headers to send with every request.
	Headers map[string]string

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *stdhttp.Client
}

// New creates a new TypeSafe AI provider.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("TYPESAFE_AI_BASE_URL")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{config: cfg, client: client}
}

// CreateTypeSafeAI creates a new TypeSafe AI provider. Mirrors TypeScript's
// createTypeSafeAi export while New remains the idiomatic Go constructor.
func CreateTypeSafeAI(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string { return "typesafe" }

// resolveHeaders returns the request headers for a call: a Bearer token
// (from Config.APIKey, or the TYPESAFE_AI_API_KEY environment variable when
// unset) plus any configured custom headers. Resolved per call, mirroring
// TypeScript's lazy `loadApiKey` inside the provider's headers() closure.
func (p *Provider) resolveHeaders() (map[string]string, error) {
	apiKey := p.config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("TYPESAFE_AI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("TypeSafe AI API key is missing. Pass it using the 'APIKey' config, or set the TYPESAFE_AI_API_KEY environment variable.") //nolint:staticcheck // matches TS SDK's exact error text
	}

	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	for k, v := range p.config.Headers {
		headers[k] = v
	}
	return version.WithUserAgentSuffix(headers, version.ProviderUserAgent("typesafe-ai")), nil
}

// DecisionModel returns a decision model by ID. Mirrors TypeScript's
// TypeSafeAiProvider.decisionModel.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	return NewDecisionModel(p, modelID), nil
}

// LanguageModel is unsupported; TypeSafe AI only implements DecisionModel.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("typesafe: no such languageModel: %s", modelID)
}

// EmbeddingModel is unsupported; TypeSafe AI only implements DecisionModel.
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("typesafe: no such embeddingModel: %s", modelID)
}

// ImageModel is unsupported; TypeSafe AI only implements DecisionModel.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("typesafe: no such imageModel: %s", modelID)
}

// SpeechModel is unsupported; TypeSafe AI only implements DecisionModel.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("typesafe does not support speech synthesis")
}

// TranscriptionModel is unsupported; TypeSafe AI only implements DecisionModel.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("typesafe does not support transcription")
}

// RerankingModel is unsupported; TypeSafe AI only implements DecisionModel.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("typesafe does not support reranking")
}

// EvaluationModel is the deprecated form of DecisionModel.
//
// Deprecated: Use DecisionModel instead.
func (p *Provider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	model, err := p.DecisionModel(modelID)
	if err != nil {
		return nil, err
	}
	return provider.AsEvaluationModel(model), nil
}
