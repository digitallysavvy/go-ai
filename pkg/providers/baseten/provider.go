package baseten

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// DefaultBaseURL is Baseten's Model APIs base URL (TS defaultBaseURL).
const DefaultBaseURL = "https://inference.baseten.co/v1"

// maxEmbeddingsPerCall is Baseten's per-request embedding input limit: larger
// batches are rejected with `413 batch size N > maximum allowed batch size
// 128`. Mirrors TS MAX_EMBEDDINGS_PER_CALL.
const maxEmbeddingsPerCall = 128

// Provider implements the provider.Provider interface for Baseten.
// Baseten is OpenAI-compatible, so this builds on the OpenAI implementation.
type Provider struct {
	*openai.Provider

	// modelURL is Config.ModelURL, kept to decide chat vs. embedding routing.
	modelURL string

	// embedding is a separate OpenAI-compatible provider instance pointed at
	// the embedding endpoint (only set when ModelURL is a /sync or /sync/v1
	// URL), since embeddings use a different provider name/base URL than chat.
	embedding *openai.Provider
}

// Config contains configuration for the Baseten provider.
type Config struct {
	// APIKey is the Baseten API key. Defaults to the BASETEN_API_KEY
	// environment variable.
	APIKey string

	// BaseURL is the base URL for the Model APIs (optional).
	// Default: https://inference.baseten.co/v1
	BaseURL string

	// ModelURL is the URL for a custom model deployment (chat or
	// embeddings). If not supplied, the default Model APIs are used for
	// chat, and EmbeddingModel returns an error (Baseten has no default
	// embeddings endpoint).
	//
	// Chat requires a /sync/v1 URL; a /predict URL returns an error, since
	// that endpoint is not an OpenAI-compatible Chat Completions API.
	// Embeddings require a /sync or /sync/v1 URL.
	ModelURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// New creates a new Baseten provider. Baseten uses an OpenAI-compatible API.
func New(cfg Config) *Provider {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("BASETEN_API_KEY")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	chatBaseURL := baseURL
	if cfg.ModelURL != "" {
		chatBaseURL = cfg.ModelURL
	}

	chatProvider := openai.New(openai.Config{
		APIKey:           apiKey,
		BaseURL:          chatBaseURL,
		Headers:          cfg.Headers,
		HTTPClient:       cfg.HTTPClient,
		ChatProviderName: "baseten.chat",
		// TS createChatModel builds an OpenAICompatibleChatLanguageModel,
		// which supports video_url content parts (7dd9ec320c).
		AllowVideo: true,
	})

	p := &Provider{
		Provider: chatProvider,
		modelURL: cfg.ModelURL,
	}

	if cfg.ModelURL != "" && strings.Contains(cfg.ModelURL, "/sync") {
		p.embedding = openai.New(openai.Config{
			APIKey:     apiKey,
			BaseURL:    basetenEmbeddingURL(cfg.ModelURL),
			Headers:    cfg.Headers,
			HTTPClient: cfg.HTTPClient,
			Name:       "baseten.embedding",
		})
	}

	return p
}

// basetenEmbeddingURL mirrors TS getCommonModelConfig's embedding URL
// handling: a /sync URL that isn't already /sync/v1 needs /v1 appended so the
// OpenAI-compatible embeddings path resolves correctly.
func basetenEmbeddingURL(modelURL string) string {
	if strings.Contains(modelURL, "/sync") && !strings.Contains(modelURL, "/sync/v1") {
		return modelURL + "/v1"
	}
	return modelURL
}

// LanguageModel returns an OpenAI-compatible Chat Completions model.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if p.modelURL != "" && strings.Contains(p.modelURL, "/predict") {
		return nil, fmt.Errorf("baseten: not supported; you must use a /sync/v1 endpoint for chat models")
	}
	if modelID == "" {
		// TS: modelId ?? 'placeholder' for a custom /sync/v1 deployment
		// (dedicated single-model endpoints ignore the "model" wire field),
		// modelId ?? 'chat' for the default Model APIs.
		if strings.Contains(p.modelURL, "/sync/v1") {
			modelID = "placeholder"
		} else {
			modelID = "chat"
		}
	}
	return p.Provider.ChatModel(modelID)
}

// ChatModel is an alias for LanguageModel (TS provider.chatModel).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an embedding model. Baseten has no default
// embeddings endpoint; Config.ModelURL must be set to a /sync or /sync/v1
// deployment URL (TS: "No model URL provided for embeddings...").
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if p.embedding == nil {
		if p.modelURL == "" {
			return nil, fmt.Errorf("baseten: no model URL provided for embeddings; set ModelURL to a /sync or /sync/v1 endpoint")
		}
		return nil, fmt.Errorf("baseten: not supported; you must use a /sync or /sync/v1 endpoint for embeddings")
	}
	if modelID == "" {
		modelID = "embeddings"
	}
	base, err := p.embedding.EmbeddingModel(modelID)
	if err != nil {
		return nil, err
	}
	openaiEmbedding, ok := base.(*openai.EmbeddingModel)
	if !ok {
		return base, nil
	}
	return &embeddingModel{EmbeddingModel: openaiEmbedding}, nil
}

// TextEmbeddingModel is an alias for EmbeddingModel (TS
// provider.textEmbeddingModel, deprecated in favor of embeddingModel).
func (p *Provider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// embeddingModel overrides the inherited OpenAI-compatible embedding model's
// MaxEmbeddingsPerCall with Baseten's lower per-request batch limit.
type embeddingModel struct {
	*openai.EmbeddingModel
}

func (m *embeddingModel) MaxEmbeddingsPerCall() int {
	return maxEmbeddingsPerCall
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "baseten"
}
