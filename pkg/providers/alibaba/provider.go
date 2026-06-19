package alibaba

import (
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for Alibaba Cloud (Qwen)
type Provider struct {
	config          Config
	client          *http.Client
	videoClient     *http.Client
	embeddingClient *http.Client
}

// New creates a new Alibaba Cloud provider with the given configuration
func New(cfg Config) *Provider {
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("ALIBABA_API_KEY")
	}

	// Chat API base URL (OpenAI-compatible endpoint)
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	}

	// Video API base URL (DashScope native endpoint)
	videoBaseURL := cfg.VideoBaseURL
	if videoBaseURL == "" {
		videoBaseURL = "https://dashscope-intl.aliyuncs.com"
	}
	embeddingBaseURL := cfg.EmbeddingBaseURL
	if embeddingBaseURL == "" {
		embeddingBaseURL = "https://dashscope-intl.aliyuncs.com/api/v1"
	}

	headers := version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
		"Content-Type":  "application/json",
	}, cfg.Headers), version.ProviderUserAgent("alibaba"))

	// Create HTTP client for chat API
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: headers,
	})

	// Create HTTP client for video API
	videoClient := http.NewClient(http.Config{
		BaseURL: videoBaseURL,
		Headers: headers,
	})
	embeddingClient := http.NewClient(http.Config{
		BaseURL: embeddingBaseURL,
		Headers: headers,
	})

	return &Provider{
		config:          cfg,
		client:          client,
		videoClient:     videoClient,
		embeddingClient: embeddingClient,
	}
}

// CreateAlibaba creates a new Alibaba Cloud provider.
//
// It mirrors the TypeScript SDK createAlibaba export while New remains the
// idiomatic Go constructor.
func CreateAlibaba(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "alibaba"
}

// LanguageModel returns a language model by ID.
// Alibaba's TypeScript SDK model ID type allows current known IDs and custom
// strings; mirror that here by accepting any non-empty model ID.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = "qwen-plus" // Default model
	}

	return NewLanguageModel(p, modelID), nil
}

// VideoModel returns a video generation model by ID
// Supported models: wan2.5-t2v, wan2.6-t2v, wan2.6-i2v, wan2.6-i2v-flash, wan2.6-r2v, wan2.6-r2v-flash
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = "wan2.6-t2v" // Default video model
	}

	// Validate model ID
	validModels := map[string]bool{
		"wan2.5-t2v":       true,
		"wan2.6-t2v":       true,
		"wan2.6-i2v":       true,
		"wan2.6-i2v-flash": true,
		"wan2.6-r2v":       true,
		"wan2.6-r2v-flash": true,
	}

	if !validModels[modelID] {
		return nil, fmt.Errorf("unsupported Alibaba video model: %s (supported: wan2.5-t2v, wan2.6-t2v, wan2.6-i2v, wan2.6-i2v-flash, wan2.6-r2v, wan2.6-r2v-flash)", modelID)
	}

	return NewVideoModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return NewEmbeddingModel(p, modelID), nil
}

// Embedding returns an embedding model by ID.
func (p *Provider) Embedding(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("LAlibaba provider does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("LAlibaba provider does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("LAlibaba provider does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("LAlibaba provider does not support reranking")
}

// Client returns the HTTP client for chat API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// VideoClient returns the HTTP client for video API requests
func (p *Provider) VideoClient() *http.Client {
	return p.videoClient
}

// EmbeddingClient returns the HTTP client for embedding API requests.
func (p *Provider) EmbeddingClient() *http.Client {
	return p.embeddingClient
}
