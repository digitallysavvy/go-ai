// Package gmicloud implements the GMI Cloud provider (GPU inference for
// open-weight models over an OpenAI-compatible API). It mirrors
// @ai-sdk/gmicloud at ai@7.0.113.
package gmicloud

import (
	"bytes"
	"context"
	"encoding/json"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

	// APIKeyFunc resolves the API key for every request instead of APIKey
	// (TS `apiKey` as a function, 7716e189ee). Its error fails the request;
	// its result is sent as is, an empty string included, and the
	// environment is not read. Setting both APIKey and APIKeyFunc fails
	// every request with an InvalidArgumentError.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

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
	// TS withoutTrailingSlash(baseURL): a trailing "/" must not double up with request paths.
	cfg.BaseURL = providerutils.WithoutTrailingSlash(cfg.BaseURL)
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	apiKey := cfg.APIKey
	if apiKey == "" && cfg.APIKeyFunc == nil {
		apiKey = os.Getenv("GMI_CLOUD_APIKEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		}, cfg.Headers), version.ProviderUserAgent("gmicloud")),
		HTTPClient: withGmicloudIncludeUsage(cfg.HTTPClient),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
			if cfg.APIKeyFunc != nil {
				return providerutils.RequestAuthHeaderFunc(ctx, cfg.Headers, providerutils.ResolveAPIKeyOptions{
					APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, EnvVar: "GMI_CLOUD_APIKEY", Description: "GMI Cloud API key",
				}, "Authorization", "Bearer ")
			}
			return providerutils.RequestAuthHeader(cfg.Headers, apiKey, "GMI_CLOUD_APIKEY", "GMI Cloud API key", "Authorization", "Bearer ")
		},
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
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an error: GMI Cloud does not support image generation
// (TS createGmicloud throws NoSuchModelError for imageModel).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns an error: GMI Cloud does not support speech synthesis.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns an error: GMI Cloud does not support
// transcription.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns an error: GMI Cloud does not support reranking.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
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

// RoundTrip closes the request body on every error, as the
// http.RoundTripper contract requires (also before the base transport runs).
func (t gmicloudIncludeUsageTransport) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	resp, err := t.roundTrip(req)
	if err != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	return resp, err
}

func (t gmicloudIncludeUsageTransport) roundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	if req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, internalhttp.RejectFetch(err)
	}
	_ = req.Body.Close()
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		// buildRequestBody already sends stream_options.include_usage, so a
		// streaming body that carries stream_options is passed through
		// byte for byte: re-marshaling HTML-escaped <, > and & and sorted
		// ordered objects (TS JSON.stringify keeps both, gmicloud-provider.ts:87).
		if _, has := payload["stream_options"]; has {
			payload = nil
		}
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
