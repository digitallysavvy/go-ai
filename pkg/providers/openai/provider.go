package openai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default OpenAI API base URL
	DefaultBaseURL = "https://api.openai.com/v1"
)

// Provider implements the provider.Provider interface for OpenAI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the OpenAI provider
type Config struct {
	// APIKey is the OpenAI API key
	APIKey string

	// BaseURL is the base URL for the OpenAI API (default: https://api.openai.com/v1)
	BaseURL string

	// Organization is the optional organization ID
	Organization string

	// Project is the optional project ID
	Project string
}

// New creates a new OpenAI provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// Create HTTP client with default headers
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", cfg.APIKey),
	}

	if cfg.Organization != "" {
		headers["OpenAI-Organization"] = cfg.Organization
	}

	if cfg.Project != "" {
		headers["OpenAI-Project"] = cfg.Project
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "openai"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
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
	if modelID == "" {
		modelID = "dall-e-3"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	if modelID == "" {
		modelID = "tts-1"
	}

	return NewSpeechModel(p, modelID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		modelID = "whisper-1"
	}

	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// OpenAI doesn't provide reranking models
	return nil, fmt.Errorf("OpenAI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
