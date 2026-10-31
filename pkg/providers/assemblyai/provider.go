package assemblyai

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for AssemblyAI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the AssemblyAI provider
type Config struct {
	// APIKey is the AssemblyAI API key
	APIKey string

	// BaseURL is the base URL for the AssemblyAI API (optional)
	BaseURL string

	// Headers are custom HTTP headers sent with every request (TS provider
	// settings `headers`).
	Headers map[string]string `json:"headers,omitempty"`
}

// New creates a new AssemblyAI provider with the given configuration
func New(cfg Config) *Provider {
	// TS loadApiKey: the key defaults to the ASSEMBLYAI_API_KEY environment variable.
	cfg.APIKey = providerutils.APIKeyOrEnv(cfg.APIKey, "ASSEMBLYAI_API_KEY")
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.assemblyai.com/v2"
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(map[string]string{
			"Authorization": cfg.APIKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("assemblyai")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "ASSEMBLYAI_API_KEY", "AssemblyAI", "Authorization", "")
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "assemblyai"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: "unknown", ModelType: "languageModel", Message: "AssemblyAI does not provide language models"}
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		modelID = "best"
	}

	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}
