// Package minimax implements the MiniMax provider's chat/language-model and
// video-generation surfaces. It mirrors @ai-sdk/minimax at ai@7.0.113.
//
// MiniMax chat delegates to the Anthropic Messages protocol: TS
// createMiniMax constructs an AnthropicLanguageModel directly against
// MiniMax's Anthropic-compatible endpoint. This Go package does the same by
// embedding pkg/providers/anthropic, adding only the per-call
// providerOptions.minimax.thinking -> Anthropic ModelOptions.Thinking bridge
// (see language_model.go) that the shared Anthropic provider does not do on
// its own (its thinking option is resolved once at model construction, not
// per call).
package minimax

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// DefaultBaseURL is MiniMax's Anthropic-compatible chat endpoint.
const DefaultBaseURL = "https://api.minimax.io/anthropic/v1"

// DefaultVideoBaseURL is MiniMax's video generation API root.
const DefaultVideoBaseURL = "https://api.minimax.io"

// Provider implements the provider.Provider interface for MiniMax chat
// models, plus a VideoModel method for MiniMax's video generation API.
type Provider struct {
	config            Config
	anthropicProvider *anthropic.Provider

	// videoClient and videoReqHeaders serve the video generation API
	// (Authorization: Bearer <apiKey>, no anthropic-version header, unlike
	// the chat client). videoReqHeaders is forwarded to VideoModel.DoStatus's
	// fileutil-based polling, which does not go through videoClient.
	videoClient     *internalhttp.Client
	videoReqHeaders map[string]string
	videoBaseURL    string
}

// Config contains configuration for the MiniMax provider.
type Config struct {
	// APIKey is the MiniMax API key. Defaults to the MINIMAX_API_KEY
	// environment variable.
	APIKey string

	// BaseURL is the base URL for chat API calls. Defaults to MiniMax's
	// Anthropic-compatible endpoint (https://api.minimax.io/anthropic/v1).
	BaseURL string

	// VideoBaseURL is the base URL for video generation API calls. Defaults
	// to https://api.minimax.io.
	VideoBaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string
}

// New creates a new MiniMax provider with the given configuration. If
// Config.APIKey is empty, the API key is loaded from the MINIMAX_API_KEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	videoBaseURL := cfg.VideoBaseURL
	if videoBaseURL == "" {
		videoBaseURL = DefaultVideoBaseURL
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("MINIMAX_API_KEY")
	}

	anthropicProvider := anthropic.New(anthropic.Config{
		APIKey:  apiKey,
		Name:    "minimax",
		BaseURL: baseURL,
		Headers: cfg.Headers,
		// TS sets supportedUrls: () => ({}) for MiniMax: unlike direct
		// Anthropic, it never accepts https image/PDF URLs directly and
		// always converts to base64.
		SupportedURLs: func(string) map[string][]string {
			return map[string][]string{}
		},
	})

	videoReqHeaders := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}
	for k, v := range cfg.Headers {
		videoReqHeaders[k] = v
	}
	videoClient := internalhttp.NewClient(internalhttp.Config{
		BaseURL: videoBaseURL,
		Headers: videoReqHeaders,
	})

	return &Provider{
		config:            cfg,
		anthropicProvider: anthropicProvider,
		videoClient:       videoClient,
		videoReqHeaders:   videoReqHeaders,
		videoBaseURL:      videoBaseURL,
	}
}

// CreateMiniMax creates a new MiniMax provider.
//
// It mirrors the TypeScript SDK createMiniMax export while New remains the
// idiomatic Go constructor.
func CreateMiniMax(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "minimax"
}

// LanguageModel returns a MiniMax chat model by ID.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p, modelID), nil
}

// ChatModel is an alias for LanguageModel (TS provider.chat).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an error: MiniMax does not support embeddings (TS
// createMiniMax throws NoSuchModelError for embeddingModel).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("minimax provider does not support embedding models (no such model: %q)", modelID)
}

// ImageModel returns an error: MiniMax does not support image generation (TS
// createMiniMax throws NoSuchModelError for imageModel).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("minimax provider does not support image models (no such model: %q)", modelID)
}

// SpeechModel returns an error: MiniMax's chat surface does not support
// speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("minimax provider does not support speech synthesis")
}

// TranscriptionModel returns an error: MiniMax's chat surface does not
// support transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("minimax provider does not support transcription")
}

// RerankingModel returns an error: MiniMax does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("minimax provider does not support reranking")
}

// VideoModel returns a MiniMax video generation model by ID (TS
// provider.video / provider.videoModel).
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required")
	}
	return newVideoModel(p, modelID), nil
}
