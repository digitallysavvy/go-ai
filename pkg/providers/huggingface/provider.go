// Package huggingface implements the Hugging Face provider backed entirely by
// the Hugging Face Router Responses API (https://router.huggingface.co/v1/responses),
// mirroring @ai-sdk/huggingface at ai@7.0.118. Unlike the legacy Hugging Face
// Inference API, the Responses API does not offer embeddings or image
// generation, so EmbeddingModel/ImageModel intentionally return errors
// (TS createHuggingFace throws NoSuchModelError for both).

package huggingface

import (
	"fmt"
	stdhttp "net/http"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// DefaultBaseURL is the default Hugging Face Router base URL.
const DefaultBaseURL = "https://router.huggingface.co/v1"

// hfProviderOptionsKey is the literal provider-options / provider-metadata
// namespace key used by the Hugging Face Responses model. It matches the TS
// source's hardcoded 'huggingface' string (distinct from Provider(), which
// returns "huggingface.responses" like TS's config.provider).
const hfProviderOptionsKey = "huggingface"

// Provider implements the provider.Provider interface for Hugging Face.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the Hugging Face provider.
type Config struct {
	// APIKey is the Hugging Face API token. Defaults to the
	// HUGGINGFACE_API_KEY environment variable.
	APIKey string

	// BaseURL is the base URL for the Hugging Face Router API. Defaults to
	// https://router.huggingface.co/v1.
	BaseURL string

	// Headers are custom HTTP headers to include in every request.
	Headers map[string]string

	// HTTPClient overrides the HTTP client used for all requests.
	HTTPClient *stdhttp.Client

	// GenerateID generates IDs for synthesized content (e.g. url citation
	// sources). Defaults to a random ID generator. Tests may inject a
	// deterministic generator, mirroring TS's config.generateId.
	GenerateID func() string
}

// New creates a new Hugging Face provider with the given configuration. If
// Config.APIKey is empty, the API key is loaded from the HUGGINGFACE_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("HUGGINGFACE_API_KEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
		}, cfg.Headers),
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateHuggingFace creates a new Hugging Face provider.
//
// It mirrors the TypeScript SDK createHuggingFace export while New remains
// the idiomatic Go constructor.
func CreateHuggingFace(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "huggingface"
}

// LanguageModel returns a Hugging Face Responses API language model by ID
// (TS provider.languageModel / provider(modelId)).
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p, modelID), nil
}

// ResponsesModel is an alias for LanguageModel (TS provider.responses).
func (p *Provider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an error: the Hugging Face Responses API does not
// support text embeddings (TS createHuggingFace throws NoSuchModelError with
// this exact message for embeddingModel/textEmbeddingModel).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("Hugging Face Responses API does not support text embeddings. Use the Hugging Face Inference API directly for embeddings.") //nolint:staticcheck // matches TS SDK's exact error text
}

// ImageModel returns an error: the Hugging Face Responses API does not
// support image generation (TS createHuggingFace throws NoSuchModelError
// with this exact message for imageModel).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("Hugging Face Responses API does not support image generation. Use the Hugging Face Inference API directly for image models.") //nolint:staticcheck // matches TS SDK's exact error text
}

// SpeechModel returns an error: Hugging Face does not provide a unified
// speech synthesis API through the Responses model.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("huggingface provider does not support speech synthesis")
}

// TranscriptionModel returns an error: Hugging Face does not provide a
// unified transcription API through the Responses model.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("huggingface provider does not support transcription")
}

// RerankingModel returns an error: Hugging Face does not support reranking
// through this interface.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("huggingface provider does not support reranking")
}

// Client returns the HTTP client used for API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

// generateID returns a new ID using the configured generator, or a random
// default when none was configured.
func (p *Provider) generateID() string {
	if p.config.GenerateID != nil {
		return p.config.GenerateID()
	}
	return streaming.GenerateID()
}
