package klingai

import (
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const defaultBaseURL = "https://api-singapore.klingai.com"

// Provider implements the provider.Provider interface for KlingAI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the KlingAI provider
type Config struct {
	// AccessKey is the KlingAI access key
	// If empty, will use KLINGAI_ACCESS_KEY environment variable
	AccessKey string

	// SecretKey is the KlingAI secret key
	// If empty, will use KLINGAI_SECRET_KEY environment variable
	SecretKey string

	// BaseURL is the base URL for the KlingAI API (optional)
	// Defaults to https://api-singapore.klingai.com
	BaseURL string

	// Headers are additional headers to include in requests
	Headers map[string]string
}

// New creates a new KlingAI provider with the given configuration
func New(cfg Config) (*Provider, error) {
	// Load access key from env if not provided
	accessKey := cfg.AccessKey
	if accessKey == "" {
		accessKey = os.Getenv("KLINGAI_ACCESS_KEY")
	}
	if accessKey == "" {
		return nil, fmt.Errorf("KlingAI access key is required (set KLINGAI_ACCESS_KEY or provide Config.AccessKey)")
	}

	// Load secret key from env if not provided
	secretKey := cfg.SecretKey
	if secretKey == "" {
		secretKey = os.Getenv("KLINGAI_SECRET_KEY")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("KlingAI secret key is required (set KLINGAI_SECRET_KEY or provide Config.SecretKey)")
	}

	// Use default base URL if not provided
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	// Store credentials in config
	cfg.AccessKey = accessKey
	cfg.SecretKey = secretKey
	cfg.BaseURL = baseURL

	// Create HTTP client (auth header will be added per-request)
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: cfg.Headers,
	})

	return &Provider{
		config: cfg,
		client: client,
	}, nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "klingai"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("KlingAI does not support language models")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("KlingAI does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("KlingAI does not support image generation")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required")
	}

	return newVideoModel(p, modelID)
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("KlingAI does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("KlingAI does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("KlingAI does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// GenerateAuthToken generates a JWT authentication token
func (p *Provider) GenerateAuthToken() (string, error) {
	return generateJWTToken(p.config.AccessKey, p.config.SecretKey)
}
