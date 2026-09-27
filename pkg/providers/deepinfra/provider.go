package deepinfra

import (
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// DefaultBaseURL is DeepInfra's OpenAI-compatible chat base URL. Image
// generation uses a sibling "/inference" root instead of "/openai"
// (deriveDeepInfraImageBaseURL); image editing shares this chat base URL
// (TS: baseURL ?? 'https://api.deepinfra.com/v1', with "/openai"/"/inference"
// suffixes — Config.BaseURL here is the already-suffixed chat URL, matching
// this package's pre-existing convention).
const DefaultBaseURL = "https://api.deepinfra.com/v1/openai"

// Provider implements the provider.Provider interface for DeepInfra.
// Chat is OpenAI-compatible, so it builds on the OpenAI implementation with
// custom fixes for token counting issues. Image generation is DeepInfra's
// own protocol; only image editing is OpenAI-compatible.
type Provider struct {
	*openai.Provider

	apiKey       string
	imageBaseURL string
	editBaseURL  string
	headers      map[string]string
}

// Config contains configuration for the DeepInfra provider
type Config struct {
	// APIKey is the DeepInfra API key
	APIKey string

	// BaseURL is the base URL for the DeepInfra chat API (optional).
	// Default: https://api.deepinfra.com/v1/openai
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string
}

// New creates a new DeepInfra provider
// DeepInfra uses OpenAI-compatible API
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	openaiProvider := openai.New(openai.Config{
		APIKey:  cfg.APIKey,
		BaseURL: baseURL,
		Headers: cfg.Headers,
	})

	return &Provider{
		Provider:     openaiProvider,
		apiKey:       cfg.APIKey,
		imageBaseURL: deriveDeepInfraImageBaseURL(baseURL),
		editBaseURL:  baseURL,
		headers:      cfg.Headers,
	}
}

// deriveDeepInfraImageBaseURL swaps a chat base URL's "/openai" suffix for
// DeepInfra's image generation root, "/inference" (TS: baseURL + '/inference'
// vs. baseURL + '/openai', both built from the same root). When the chat
// base URL doesn't end in "/openai" (e.g. a caller pointed it at a custom
// endpoint entirely), "/inference" is appended to it as a best-effort default
// rather than guessing at an unrelated root.
func deriveDeepInfraImageBaseURL(chatBaseURL string) string {
	trimmed := strings.TrimRight(chatBaseURL, "/")
	if root, ok := strings.CutSuffix(trimmed, "/openai"); ok {
		return root + "/inference"
	}
	return trimmed + "/inference"
}

func (p *Provider) requestHeaders() map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + p.apiKey,
		"Content-Type":  "application/json",
	}
	for k, v := range p.headers {
		headers[k] = v
	}
	return headers
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "deepinfra"
}

// LanguageModel returns a language model with DeepInfra-specific fixes
// This includes token counting corrections for Gemini/Gemma models
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p.Provider, modelID), nil
}

// ImageModel returns a DeepInfra image generation model.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return NewImageModel(p, modelID, p.imageBaseURL, p.editBaseURL), nil
}

// imageClient builds a client pointed at the given base URL, sharing this
// provider's API key/headers (TS getCommonModelConfig-equivalent for the
// image model, which is not OpenAI-compatible and so cannot reuse
// *openai.Provider's client).
func (p *Provider) imageClient(baseURL string) *internalhttp.Client {
	return internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: p.requestHeaders(),
	})
}
