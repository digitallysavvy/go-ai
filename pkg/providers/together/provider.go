package together

import (
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Together AI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Together AI provider
type Config struct {
	// APIKey is the Together AI API key
	APIKey string

	// BaseURL is the base URL for the Together AI API (optional)
	BaseURL string
}

// getAPIKey resolves the Together AI API key.
//
// Resolution order:
//  1. Explicit value from cfg.APIKey
//  2. TOGETHER_API_KEY environment variable (primary)
//  3. TOGETHER_AI_API_KEY environment variable (deprecated fallback)
func getAPIKey(apiKey string) string {
	if apiKey != "" {
		return apiKey
	}
	if key := os.Getenv("TOGETHER_API_KEY"); key != "" {
		return key
	}
	// Deprecated: TOGETHER_AI_API_KEY will be removed in a future release.
	// Use TOGETHER_API_KEY instead.
	if key := os.Getenv("TOGETHER_AI_API_KEY"); key != "" {
		log.Println("together: TOGETHER_AI_API_KEY is deprecated and will be removed in a future release. Please use TOGETHER_API_KEY instead.")
		return key
	}
	return ""
}

// New creates a new Together AI provider with the given configuration.
// If Config.APIKey is empty, the API key is loaded from the TOGETHER_API_KEY
// environment variable. TOGETHER_AI_API_KEY is accepted as a deprecated fallback.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.together.xyz"
	}

	apiKey := getAPIKey(cfg.APIKey)

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "together"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "mistralai/Mixtral-8x7B-Instruct-v0.1"
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		modelID = "togethercomputer/m2-bert-80M-8k-retrieval"
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "stabilityai/stable-diffusion-xl-base-1.0"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Together AI does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Together AI does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Together AI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
