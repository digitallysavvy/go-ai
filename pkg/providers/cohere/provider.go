package cohere

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const (
	// DefaultBaseURL is the default Cohere API base URL (TS createCohere
	// baseURL). Endpoint paths (/chat, /embed, /rerank) are relative to it.
	DefaultBaseURL = "https://api.cohere.com/v2"
)

// Provider implements the provider.Provider interface for Cohere
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Cohere provider
type Config struct {
	// APIKey is the Cohere API key
	APIKey string

	// BaseURL is the base URL for the Cohere API including the version
	// (default: https://api.cohere.com/v2). A host-only value such as
	// "https://api.cohere.com" gets "/v2" appended, as v0.5.2 callers expected.
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new Cohere provider with the given configuration
func New(cfg Config) *Provider {
	// TS loadApiKey: the key defaults to the COHERE_API_KEY environment variable.
	cfg.APIKey = providerutils.APIKeyOrEnv(cfg.APIKey, "COHERE_API_KEY")
	baseURL := normalizeBaseURL(cfg.BaseURL)

	// Create HTTP client with default headers
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", cfg.APIKey),
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(headers, cfg.Headers), version.ProviderUserAgent("cohere")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "COHERE_API_KEY", "Cohere", "Authorization", "Bearer ")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateCohere creates a new Cohere provider.
//
// It mirrors the TypeScript SDK createCohere export while New remains the
// idiomatic Go constructor.
func CreateCohere(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "cohere"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID with default options
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// EmbeddingModelWithOptions returns an embedding model by ID with custom options
func (p *Provider) EmbeddingModelWithOptions(modelID string, options EmbeddingOptions) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewEmbeddingModel(p, modelID, options), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("cohere does not support image generation")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("cohere does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("cohere does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewRerankingModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// normalizeBaseURL applies the default and keeps host-only base URLs working:
// v0.5.2 appended "/v2/chat" to the configured base, so a base without a path
// meant "<host>/v2".
func normalizeBaseURL(base string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return DefaultBaseURL
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" && u.Path == "" {
		return base + "/v2"
	}
	return base
}
