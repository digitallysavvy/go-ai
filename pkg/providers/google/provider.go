package google

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		Headers:    headers,
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
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

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Google doesn't provide transcription through this API
	return nil, fmt.Errorf("LGoogle does not support transcription through this API")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Google doesn't provide reranking models
	return nil, fmt.Errorf("LGoogle does not support reranking")
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
	if p.config.APIKey != "" {
		return p.config.APIKey
	}
	return os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
}

func (p *Provider) Files() provider.FilesAPI {
	return &FilesAPI{provider: p}
}
