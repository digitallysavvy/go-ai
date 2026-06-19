package googlevertex

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"os"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	anthropicprovider "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	googleprovider "github.com/digitallysavvy/go-ai/pkg/providers/google"
	vertexanthropic "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/version"
	"golang.org/x/oauth2"
)

const (
	// DefaultBaseURL is the default Google Vertex AI API base URL template
	// {region} and {project} will be replaced with actual values
	DefaultBaseURLTemplate = "https://{region}-aiplatform.googleapis.com/v1beta1/projects/{project}/locations/{region}/publishers/google"
)

// Provider implements the provider.Provider interface for Google Vertex AI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Google Vertex AI provider
type Config struct {
	// APIKey enables Vertex express-mode authentication (x-goog-api-key).
	// When set, AccessToken/AuthToken/TokenSource are ignored.
	APIKey string

	// Project is the Google Cloud project ID
	Project string

	// Location is the Google Cloud location (e.g., "us-central1")
	Location string

	// AccessToken is the OAuth2 access token for authentication
	// This can be obtained from Google Cloud SDK or service account
	// Deprecated: prefer AuthToken or TokenSource for automatic refresh.
	AccessToken string

	// AuthToken returns a bearer token per request. When set, it overrides
	// AccessToken and TokenSource.
	AuthToken func(ctx context.Context) (string, error) `json:"-"`

	// TokenSource provides OAuth2 tokens. Used when AuthToken and AccessToken
	// are not set.
	TokenSource oauth2.TokenSource `json:"-"`

	// BaseURL is the base URL for the Vertex AI API (optional, computed from project/location if not provided)
	BaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

type cachedTokenSource struct {
	mu      sync.Mutex
	token   string
	expiry  time.Time
	resolve func(ctx context.Context) (string, time.Time, error)
}

func (c *cachedTokenSource) tokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.token != "" && now.Before(c.expiry.Add(-5*time.Minute)) {
		return c.token, nil
	}
	token, expiry, err := c.resolve(ctx)
	if err != nil {
		return "", err
	}
	c.token = token
	c.expiry = expiry
	return token, nil
}

type authTransport struct {
	base      stdhttp.RoundTripper
	tokenFunc func(ctx context.Context) (string, error)
}

func (t *authTransport) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	clone := req.Clone(req.Context())
	token, err := t.tokenFunc(req.Context())
	if err != nil {
		return nil, err
	}
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(clone)
}

// New creates a new Google Vertex AI provider with the given configuration
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("GOOGLE_VERTEX_API_KEY")
	}
	if cfg.Project == "" {
		cfg.Project = os.Getenv("GOOGLE_VERTEX_PROJECT")
	}
	if cfg.Location == "" {
		cfg.Location = os.Getenv("GOOGLE_VERTEX_LOCATION")
	}

	baseURL := cfg.BaseURL
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	var httpClient *stdhttp.Client

	if cfg.APIKey != "" {
		// Vertex express mode (TS parity): API key auth and publishers/google base URL.
		if baseURL == "" {
			baseURL = "https://aiplatform.googleapis.com/v1/publishers/google"
		}
		headers["x-goog-api-key"] = cfg.APIKey
	} else {
		// Validate project/location for standard Vertex endpoints.
		if cfg.Project == "" {
			return nil, fmt.Errorf("project is required for Google Vertex AI")
		}
		if cfg.Location == "" {
			return nil, fmt.Errorf("location is required for Google Vertex AI")
		}
		if cfg.AccessToken == "" && cfg.AuthToken == nil && cfg.TokenSource == nil {
			return nil, fmt.Errorf("access token is required for Google Vertex AI")
		}
		if baseURL == "" {
			baseURL = fmt.Sprintf("https://%s/v1beta1/projects/%s/locations/%s/publishers/google",
				vertexHost(cfg.Location), cfg.Project, cfg.Location)
		}

		tokenCache := &cachedTokenSource{
			resolve: func(ctx context.Context) (string, time.Time, error) {
				if cfg.AuthToken != nil {
					token, err := cfg.AuthToken(ctx)
					if err != nil {
						return "", time.Time{}, err
					}
					return token, time.Now().Add(1 * time.Hour), nil
				}
				if cfg.AccessToken != "" {
					return cfg.AccessToken, time.Now().Add(24 * time.Hour), nil
				}
				if cfg.TokenSource != nil {
					token, err := cfg.TokenSource.Token()
					if err != nil {
						return "", time.Time{}, fmt.Errorf("failed to resolve vertex auth token: %w", err)
					}
					if token == nil || token.AccessToken == "" {
						return "", time.Time{}, fmt.Errorf("resolved empty vertex auth token")
					}
					return token.AccessToken, token.Expiry, nil
				}
				return "", time.Time{}, fmt.Errorf("access token is required for Google Vertex AI")
			},
		}
		baseTransport := stdhttp.RoundTripper(stdhttp.DefaultTransport)
		httpClient = &stdhttp.Client{
			Transport: &authTransport{
				base: baseTransport,
				tokenFunc: func(ctx context.Context) (string, error) {
					// Token override bypasses cached credentials.
					if cfg.AuthToken != nil {
						return cfg.AuthToken(ctx)
					}
					return tokenCache.tokenFor(ctx)
				},
			},
		}
	}

	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    version.WithUserAgentSuffix(http.MergeHeaders(headers, cfg.Headers), version.ProviderUserAgent("google-vertex")),
		HTTPClient: httpClient,
	})

	return &Provider{
		config: cfg,
		client: client,
	}, nil
}

func vertexHost(location string) string {
	switch location {
	case "global":
		return "aiplatform.googleapis.com"
	case "eu", "us":
		return fmt.Sprintf("aiplatform.%s.rep.googleapis.com", location)
	default:
		return fmt.Sprintf("%s-aiplatform.googleapis.com", location)
	}
}

// CreateGoogleVertex creates a new Google Vertex AI provider.
//
// It mirrors the TypeScript SDK createGoogleVertex export while New remains the
// idiomatic Go constructor.
func CreateGoogleVertex(cfg Config) (*Provider, error) {
	return New(cfg)
}

// CreateVertex creates a new Google Vertex AI provider.
//
// Deprecated: use CreateGoogleVertex.
func CreateVertex(cfg Config) (*Provider, error) {
	return CreateGoogleVertex(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "google-vertex"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// AnthropicModel returns a Claude language model routed through Vertex AI's
// Anthropic publisher endpoint.
func (p *Provider) AnthropicModel(modelID string, settings ...*anthropicprovider.ModelOptions) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	var opts *anthropicprovider.ModelOptions
	if len(settings) > 0 {
		opts = settings[0]
	}
	vertexAnthropic := vertexanthropic.NewGoogleVertexAnthropicProvider(vertexanthropic.Options{
		Project:   p.config.Project,
		Location:  p.config.Location,
		BaseURL:   p.config.BaseURL,
		Headers:   p.config.Headers,
		AuthToken: p.anthropicAuthToken,
	})
	return vertexAnthropic.LanguageModelWithOptions(modelID, opts)
}

func (p *Provider) anthropicAuthToken(ctx context.Context) (string, error) {
	if p.config.AuthToken != nil {
		return p.config.AuthToken(ctx)
	}
	if p.config.AccessToken != "" {
		return p.config.AccessToken, nil
	}
	if p.config.TokenSource != nil {
		token, err := p.config.TokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("failed to resolve vertex auth token: %w", err)
		}
		if token == nil || token.AccessToken == "" {
			return "", fmt.Errorf("resolved empty vertex auth token")
		}
		return token.AccessToken, nil
	}
	return "", fmt.Errorf("access token is required for Google Vertex AI")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a Gemini TTS speech synthesis model by ID.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return googleprovider.NewSpeechModelWithConfig(modelID, googleprovider.SpeechModelConfig{
		ProviderName:        "google.vertex.speech",
		MetadataKey:         "google",
		ProviderOptionsKeys: []string{"googleVertex", "vertex", "google"},
		GeneratePath: func(id string) string {
			return fmt.Sprintf("/models/%s:generateContent", id)
		},
		Client: p.client,
	}), nil
}

// Speech returns a Gemini TTS speech synthesis model by ID.
func (p *Provider) Speech(modelID string) (provider.SpeechModel, error) {
	return p.SpeechModel(modelID)
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("LGoogle Vertex AI does not support transcription through this API")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("LGoogle Vertex AI does not support reranking")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return nil, fmt.Errorf("video models not yet implemented for Google Vertex AI")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// Project returns the Google Cloud project ID
func (p *Provider) Project() string {
	return p.config.Project
}

// Location returns the Google Cloud location
func (p *Provider) Location() string {
	return p.config.Location
}
