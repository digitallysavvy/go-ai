// Package gmicloud implements the GMI Cloud provider (GPU inference for
// open-weight models over an OpenAI-compatible API). It mirrors
// @ai-sdk/gmicloud at ai@7.0.113.
package gmicloud

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DefaultBaseURL is the default GMI Cloud API base URL.
const DefaultBaseURL = "https://api.gmi-serving.com/v1"

// Provider implements the provider.Provider interface for GMI Cloud.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the GMI Cloud provider.
type Config struct {
	// APIKey is the GMI Cloud API key. Defaults to the GMI_CLOUD_APIKEY
	// environment variable (matching GMI's own SDK package).
	APIKey string

	// BaseURL is the base URL for API calls. Defaults to
	// https://api.gmi-serving.com/v1.
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string

	// HTTPClient overrides the HTTP client used for all requests.
	HTTPClient *stdhttp.Client
}

// New creates a new GMI Cloud provider with the given configuration. If
// Config.APIKey is empty, the API key is loaded from the GMI_CLOUD_APIKEY
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("GMI_CLOUD_APIKEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("gmicloud")),
		HTTPClient: withGmicloudIncludeUsage(cfg.HTTPClient),
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateGmicloud creates a new GMI Cloud provider.
//
// It mirrors the TypeScript SDK createGmicloud export while New remains the
// idiomatic Go constructor.
func CreateGmicloud(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "gmicloud"
}

// LanguageModel returns a GMI Cloud chat model by ID.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return NewLanguageModel(p, modelID), nil
}

// ChatModel is an alias for LanguageModel (TS provider.chat).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an error: GMI Cloud does not support embeddings
// (TS createGmicloud throws NoSuchModelError for embeddingModel).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("gmicloud provider does not support embedding models (no such model: %q)", modelID)
}

// ImageModel returns an error: GMI Cloud does not support image generation
// (TS createGmicloud throws NoSuchModelError for imageModel).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("gmicloud provider does not support image models (no such model: %q)", modelID)
}

// SpeechModel returns an error: GMI Cloud does not support speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("gmicloud provider does not support speech synthesis")
}

// TranscriptionModel returns an error: GMI Cloud does not support
// transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("gmicloud provider does not support transcription")
}

// RerankingModel returns an error: GMI Cloud does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("gmicloud provider does not support reranking")
}

// Client returns the HTTP client used for API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

// withGmicloudIncludeUsage wraps client so every streaming chat completions
// request includes stream_options.include_usage: true. TS sets
// includeUsage: true explicitly on the OpenAICompatibleChatLanguageModel
// config for this reason; the Go SDK builds its own request body per
// providerutils/streaming.OpenAICompatStream conventions, so the flag is
// injected here at the HTTP transport layer instead (mirrors the
// cerebras.withCerebrasTransform pattern for provider-specific body
// rewrites).
func withGmicloudIncludeUsage(client *stdhttp.Client) *stdhttp.Client {
	if client == nil {
		client = &stdhttp.Client{}
	} else {
		clone := *client
		client = &clone
	}
	base := client.Transport
	if base == nil {
		base = stdhttp.DefaultTransport
	}
	client.Transport = gmicloudIncludeUsageTransport{base: base}
	return client
}

type gmicloudIncludeUsageTransport struct {
	base stdhttp.RoundTripper
}

func (t gmicloudIncludeUsageTransport) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	if req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		if stream, _ := payload["stream"].(bool); stream {
			payload["stream_options"] = map[string]interface{}{"include_usage": true}
			if transformed, err := json.Marshal(payload); err == nil {
				body = transformed
			}
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return t.base.RoundTrip(req)
}
