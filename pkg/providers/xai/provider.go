package xai

import (
	"fmt"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for xAI (Grok)
type Provider struct {
	config          Config
	client          *http.Client
	realtimeBaseURL string
}

// Config contains configuration for the xAI provider
type Config struct {
	// APIKey is the xAI API key
	APIKey string

	// BaseURL is the base URL for the xAI API (optional)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// getAPIKey resolves the xAI API key.
// Resolution order:
//  1. Explicit value from cfg.APIKey
//  2. XAI_API_KEY environment variable
func getAPIKey(apiKey string) string {
	if apiKey != "" {
		return apiKey
	}
	return os.Getenv("XAI_API_KEY")
}

// New creates a new xAI provider with the given configuration.
// If Config.APIKey is empty, the API key is loaded from the XAI_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	realtimeBaseURL := strings.TrimRight(cfg.BaseURL, "/")
	if realtimeBaseURL == "" {
		realtimeBaseURL = "https://api.x.ai/v1"
	}

	baseURL := normalizeBaseURL(cfg.BaseURL)
	cfg.BaseURL = baseURL

	apiKey := getAPIKey(cfg.APIKey)
	cfg.APIKey = apiKey

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: http.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		}, cfg.Headers),
	})

	return &Provider{
		config:          cfg,
		client:          client,
		realtimeBaseURL: realtimeBaseURL,
	}
}

func normalizeBaseURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.x.ai"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return strings.TrimSuffix(baseURL, "/v1")
}

// CreateXai creates a new xAI provider.
//
// It mirrors the TypeScript SDK createXai export while New remains the
// idiomatic Go constructor.
func CreateXai(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "xai"
}

// LanguageModel returns a language model by ID using the Responses API (default).
// Use ChatCompletionsLanguageModel for the legacy Chat Completions API.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "grok-beta"
	}

	return NewResponsesLanguageModel(p, modelID), nil
}

// ChatCompletionsLanguageModel returns a language model that uses the Chat Completions
// API (/v1/chat/completions). This is the legacy API path; prefer LanguageModel() for
// new code which uses the Responses API by default.
func (p *Provider) ChatCompletionsLanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "grok-beta"
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("xAI does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = ModelGrokImagineImage
	}
	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("xAI does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("xAI does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("xAI does not support reranking")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = "grok-imagine-video"
	}
	return NewVideoModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

func (p *Provider) Files() provider.FilesAPI {
	return &FilesAPI{provider: p}
}
