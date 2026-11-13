package cohere

import (
	"context"
	"fmt"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"net/url"
	"regexp"
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

	// APIKeyFunc resolves the API key before each request (TS apiKey given as
	// a function, 7716e189ee), for keys that are looked up, refreshed or
	// rotated. It cannot be combined with APIKey; the environment variable is
	// not read, and its result is sent as is.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

	// BaseURL is the base URL for the Cohere API including the version
	// (default: https://api.cohere.com/v2). A host-only value such as
	// "https://api.cohere.com" gets "/v2" appended, as v0.5.2 callers expected.
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// GenerateID generates the ids of the document sources made from chat
	// citations (TS generateId setting). Nil uses the default generator.
	GenerateID func() string `json:"-"`
}

// New creates a new Cohere provider with the given configuration
func New(cfg Config) *Provider {
	// TS loadApiKey: the key defaults to the COHERE_API_KEY environment variable.
	if cfg.APIKeyFunc == nil {
		cfg.APIKey = providerutils.APIKeyOrEnv(cfg.APIKey, "COHERE_API_KEY")
	}
	baseURL := normalizeBaseURL(cfg.BaseURL)

	// Create HTTP client with default headers
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", cfg.APIKey),
	}

	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(http.MergeHeaders(headers, cfg.Headers), version.ProviderUserAgent("cohere")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
			if cfg.APIKeyFunc != nil {
				return providerutils.RequestAuthHeaderFunc(ctx, cfg.Headers, providerutils.ResolveAPIKeyOptions{
					APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, EnvVar: "COHERE_API_KEY", Description: "Cohere",
				}, "Authorization", "Bearer ")
			}
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
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
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
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewRerankingModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// normalizeBaseURL applies the default and keeps custom base URLs working:
// v0.5.2 appended "/v2/chat" to the configured base, so a base that does not
// already end in a version segment (a host, or a proxy path) meant "<base>/v2"
// (v0.5.3 behavior, kept for compatibility).
func normalizeBaseURL(base string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	last := u.Path[strings.LastIndex(u.Path, "/")+1:]
	if versionSegment.MatchString(last) {
		return base
	}
	return base + "/v2"
}

var versionSegment = regexp.MustCompile(`^v[0-9]+$`)
