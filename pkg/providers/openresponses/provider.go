package openresponses

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Open Responses API
// This provider enables compatibility with local LLMs (LMStudio, Ollama) and
// other services that implement the OpenAI Responses API format.
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Open Responses provider
type Config struct {
	// BaseURL is the base URL for the Open Responses API endpoint
	// Examples:
	//   - LMStudio: "http://localhost:1234/v1"
	//   - Ollama: "http://localhost:11434/v1"
	//   - LocalAI: "http://localhost:8080/v1"
	BaseURL string

	// APIKey is the optional API key for authentication
	// Many local model servers don't require authentication
	APIKey string

	// Headers are custom HTTP headers to include in requests
	Headers map[string]string

	// Name is the provider name for identification (default: "open-responses")
	Name string
}

// New creates a new Open Responses provider with the given configuration
func New(cfg Config) *Provider {
	// Validate base URL
	if cfg.BaseURL == "" {
		panic("openresponses: baseURL is required")
	}

	// Set default provider name
	if cfg.Name == "" {
		cfg.Name = "open-responses"
	}

	// Build headers
	headers := make(map[string]string)
	headers["Content-Type"] = "application/json"

	// Add authorization header if API key is provided
	if cfg.APIKey != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", cfg.APIKey)
	}

	// Add custom headers
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	// Create HTTP client
	client := http.NewClient(http.Config{
		BaseURL: cfg.BaseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return p.config.Name
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support embedding models")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
