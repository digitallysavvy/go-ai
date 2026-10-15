package google

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const (
	// DefaultBaseURL is the default Google Generative AI API base URL prefix.
	DefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Provider implements the provider.Provider interface for Google (Gemini)
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the Google provider
type Config struct {
	// BatchResultDownloads configures JSON Lines batch result downloads, e.g.
	// the maximum bytes per result row (TS `batchResultDownloads`).
	BatchResultDownloads providerutils.BatchResultDownloads

	// APIKey is the Google API key
	APIKey string

	// BaseURL is the base URL for the Google API (default: https://generativelanguage.googleapis.com)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// Name overrides the provider name returned by Provider.Name().
	// Defaults to "google.generative-ai".
	Name string

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// New creates a new Google provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	cfg.BaseURL = baseURL

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	}

	headers := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
		"Content-Type":   "application/json",
		"x-goog-api-key": apiKey,
	}, cfg.Headers), version.ProviderUserAgent("google"))

	p := &Provider{config: cfg}
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		Headers:    headers,
		HTTPClient: cfg.HTTPClient,
		// TS getHeaders runs per request: the key is (re)loaded then and a
		// missing key fails the request with a LoadAPIKeyError.
		HeadersFunc: p.apiKeyHeaders,
	})

	return p
}

// apiKeyHeaders resolves the x-goog-api-key header at request time like TS
// getHeaders/loadApiKey: Config.APIKey or GOOGLE_GENERATIVE_AI_API_KEY, and a
// LoadAPIKeyError when neither is set. A non-empty x-goog-api-key in
// Config.Headers already authenticates the request and is kept.
func (p *Provider) apiKeyHeaders() (map[string]string, error) {
	for k, v := range p.config.Headers {
		if strings.EqualFold(k, "x-goog-api-key") && v != "" {
			return nil, nil
		}
	}
	key, err := providerutils.LoadAPIKey(p.config.APIKey, "GOOGLE_GENERATIVE_AI_API_KEY", "Google Generative AI")
	if err != nil {
		return nil, err
	}
	return map[string]string{"x-goog-api-key": key}, nil
}

// defaultHeaders returns the client's default headers (user-agent suffix and
// config headers) without the empty placeholder x-goog-api-key that New sets
// when no key is configured, so a differently-cased header from Config.Headers
// or the request-time key cannot collide with an empty value.
func (p *Provider) defaultHeaders() map[string]string {
	h := p.client.Headers()
	for k, v := range h {
		if v == "" {
			delete(h, k)
		}
	}
	return h
}

// headersSnapshot returns the client's default headers with the lazily
// loaded API key applied (a missing key leaves the header as configured).
func (p *Provider) headersSnapshot() map[string]string {
	h := p.client.Headers()
	if extra, err := p.apiKeyHeaders(); err == nil {
		for k, v := range extra {
			h[k] = v
		}
	}
	return h
}

// CreateGoogle creates a new Google Generative AI provider.
//
// It mirrors the TypeScript SDK createGoogle export while New remains the
// idiomatic Go constructor.
func CreateGoogle(cfg Config) *Provider {
	return New(cfg)
}

// CreateGoogleGenerativeAI creates a new Google Generative AI provider.
//
// Deprecated: use CreateGoogle.
func CreateGoogleGenerativeAI(cfg Config) *Provider {
	return CreateGoogle(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "google.generative-ai"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// Interactions returns a language model backed by the Gemini Interactions API.
func (p *Provider) Interactions(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewInteractionsLanguageModel(p, modelID), nil
}

// InteractionsAgent returns an Interactions API model for a Gemini agent preset.
func (p *Provider) InteractionsAgent(agent string) (provider.LanguageModel, error) {
	if agent == "" {
		return nil, fmt.Errorf("agent cannot be empty")
	}
	return NewInteractionsAgentModel(p, agent), nil
}

// InteractionsManagedAgent returns an Interactions API model for a
// user-defined agent created via the Agent Builder API (TS
// `google.interactions({ managedAgent: id })`).
func (p *Provider) InteractionsManagedAgent(id string) (provider.LanguageModel, error) {
	if id == "" {
		return nil, fmt.Errorf("managed agent id cannot be empty")
	}
	return NewInteractionsManagedAgentModel(p, id), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return NewSpeechModel(p, modelID), nil
}

// Speech returns a speech synthesis model by ID.
func (p *Provider) Speech(modelID string) (provider.SpeechModel, error) {
	return p.SpeechModel(modelID)
}

// TranscriptionModel returns a Gemini 3.5 Transcribe speech-to-text model by
// ID (TS `provider.transcriptionModel`/`provider.transcription`), served
// through the Interactions API. A "-live" model ID is accepted (matching TS
// GoogleTranscriptionModelId) but DoTranscribe rejects it at call time: the
// Go SDK's TranscriptionModel interface has no streaming counterpart yet.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewTranscriptionModel(p, modelID), nil
}

// Transcription is an alias for TranscriptionModel (TS `provider.transcription`).
func (p *Provider) Transcription(modelID string) (provider.TranscriptionModel, error) {
	return p.TranscriptionModel(modelID)
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Google doesn't provide reranking models
	return nil, fmt.Errorf("Google does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewVideoModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

// APIKey returns the API key
func (p *Provider) APIKey() string {
	return providerutils.APIKeyOrEnv(p.config.APIKey, "GOOGLE_GENERATIVE_AI_API_KEY")
}

func (p *Provider) Files() provider.FilesAPI {
	return &FilesAPI{provider: p}
}

// DecisionModel returns an experimental decision model backed by the
// Google Generative AI chat language model. Mirrors TypeScript's
// GoogleGenerativeAIProvider.decisionModel: `provider.decisionModel =
// (modelId) => new DecisionLanguageModel({ model: createChatModel(modelId),
// provider: providerName.replace(/\.generative-ai$/, "") + '.decision' })`.
func (p *Provider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	model, err := p.LanguageModel(modelID)
	if err != nil {
		return nil, err
	}
	providerName := strings.TrimSuffix(p.Name(), ".generative-ai") + ".decision"
	return ai.NewDecisionLanguageModel(ai.DecisionLanguageModelOptions{
		Model:    model,
		Provider: providerName,
	})
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
