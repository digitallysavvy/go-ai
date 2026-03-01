package bytedance

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const defaultBaseURL = "https://ark.ap-southeast.bytepluses.com/api/v3"

// Provider implements the provider.Provider interface for ByteDance (Volcengine)
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the ByteDance provider
type Config struct {
	// APIKey is the ByteDance Ark API key.
	// If empty, will use BYTEDANCE_API_KEY or ARK_API_KEY environment variable.
	APIKey string

	// BaseURL is the base URL for the ByteDance API (optional).
	// Defaults to https://ark.ap-southeast.bytepluses.com/api/v3
	BaseURL string

	// Headers are additional headers to include in requests
	Headers map[string]string
}

// New creates a new ByteDance provider with the given configuration
func New(cfg Config) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("BYTEDANCE_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("ARK_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ByteDance API key is required (set BYTEDANCE_API_KEY, ARK_API_KEY, or provide Config.APIKey)")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	cfg.APIKey = apiKey
	cfg.BaseURL = baseURL

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}, nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "bytedance"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("ByteDance provider does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("ByteDance provider does not support embedding models")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("ByteDance provider does not support image generation")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required")
	}
	return newVideoModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("ByteDance provider does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("ByteDance provider does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("ByteDance provider does not support reranking")
}
