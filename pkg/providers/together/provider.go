package together

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/version"
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

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
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
	baseURL = trimBaseURL(baseURL)

	apiKey := getAPIKey(cfg.APIKey)

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("togetherai")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			return providerutils.RequestAuthHeader(cfg.Headers, getAPIKey(cfg.APIKey), "TOGETHER_API_KEY", "TogetherAI", "Authorization", "Bearer ")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateTogetherAI creates a new Together AI provider.
//
// It mirrors the TypeScript SDK createTogetherAI export while New remains the
// idiomatic Go constructor.
func CreateTogetherAI(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "together"
}

// CompletionModel returns a text completion model (TS provider.completionModel,
// an OpenAICompatibleCompletionLanguageModel with includeUsage: true).
func (p *Provider) CompletionModel(modelID string) (provider.LanguageModel, error) {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.together.xyz"
	}
	return openai.New(openai.Config{
		APIKey:            getAPIKey(p.config.APIKey),
		APIKeyEnvVar:      "TOGETHER_API_KEY",
		APIKeyDescription: "TogetherAI",
		BaseURL:           trimBaseURL(baseURL) + "/v1",
		Headers:           p.config.Headers,
		Name:              "togetherai",
		OpenAICompatible:  true,
		IncludeUsage:      true,
		UserAgentName:     "togetherai",
	}).CompletionModel(modelID)
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
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if modelID == "" {
		modelID = "Salesforce/Llama-Rank-v1"
	}

	return NewRerankingModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// trimBaseURL mirrors TS withoutTrailingSlash and also accepts TS-style base
// URLs that already carry the "/v1" segment: every request path in this
// package starts with "/v1/", so ".../v1" must not double it.
func trimBaseURL(baseURL string) string {
	return strings.TrimSuffix(providerutils.WithoutTrailingSlash(baseURL), "/v1")
}
