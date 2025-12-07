package huggingface

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Hugging Face
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Hugging Face provider
type Config struct {
	// APIKey is the Hugging Face API token
	APIKey string

	// BaseURL is the base URL for the Hugging Face Inference API (optional)
	BaseURL string
}

// New creates a new Hugging Face provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api-inference.huggingface.co"
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Authorization": "Bearer " + cfg.APIKey,
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
	return "huggingface"
}

// LanguageModel returns a language model by model ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("Hugging Face requires a model ID (e.g., 'meta-llama/Llama-2-7b-chat-hf')")
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		modelID = "sentence-transformers/all-MiniLM-L6-v2"
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "stabilityai/stable-diffusion-2-1"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Hugging Face does not provide a unified speech synthesis API")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Hugging Face does not provide a unified transcription API")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Hugging Face does not support reranking through this interface")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
