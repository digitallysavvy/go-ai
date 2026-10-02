// Package googlevertex implements the Go equivalent of TS's
// @ai-sdk/google-vertex: Gemini, Anthropic (via the anthropic subpackage),
// and Grok (via the xai subpackage) models served through Google Vertex AI,
// alongside Vertex-hosted embedding, image, video, speech, and
// transcription models. Gemini-family chat/generation logic is shared with
// pkg/providers/google through pkg/providers/gemini; OAuth2 Bearer
// authentication (or Express Mode API-key auth) and Vertex's
// region/project-scoped base URLs are this package's own concern.
package googlevertex

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	anthropicprovider "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	googleprovider "github.com/digitallysavvy/go-ai/pkg/providers/google"
	vertexanthropic "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/anthropic"
	vertexinternal "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/internal"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

	// endpointClient targets the endpoint-style base URL (no
	// "/publishers/google" suffix), used for tuned models addressed as
	// "endpoints/{id}" (TS isEndpointModelId / loadBaseURL({endpoint: true})).
	// nil when the caller supplied an explicit BaseURL, matching TS: an
	// explicit baseURL is used verbatim regardless of the endpoint flag.
	endpointClient *http.Client

	// cloudTTSClient targets the (non-regional) Cloud Text-to-Speech
	// synthesize endpoint for Chirp 3: HD voices, reusing the same
	// OAuth-authenticated *stdhttp.Client as client/endpointClient. nil in
	// Express Mode (API key auth), which Chirp speech models reject outright.
	cloudTTSClient *http.Client
}

// defaultCloudTTSSynthesizeURL is the (non-regional) Cloud Text-to-Speech
// synthesize endpoint used by Chirp 3: HD voices
// (TS CLOUD_TTS_SYNTHESIZE_URL). Unlike Vertex AI and Speech-to-Text, Cloud
// Text-to-Speech has a single global host, not a per-region one.
const defaultCloudTTSSynthesizeURL = "https://texttospeech.googleapis.com/v1/text:synthesize"

// isEndpointModelID mirrors TS isEndpointModelId: tuned models are served
// from a deployed endpoint and addressed by their "endpoints/{id}" resource,
// which replaces "publishers/google/models/{id}".
// https://cloud.google.com/vertex-ai/generative-ai/docs/deploy/overview
func isEndpointModelID(modelID string) bool {
	return strings.HasPrefix(modelID, "endpoints/")
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

	// CloudTTSBaseURL overrides the Cloud Text-to-Speech synthesize endpoint
	// used by Chirp 3: HD voices (default: the real, non-regional
	// texttospeech.googleapis.com host). Primarily for tests.
	CloudTTSBaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *stdhttp.Client `json:"-"`

	// ToolResultDownloads configures downloading of remote files referenced
	// by tool-result content before sending them to Vertex as inline data
	// (Vertex function responses only accept inline data, unlike the Google
	// Generative AI API). Matches TS GoogleVertexProviderSettings.toolResultDownloads.
	ToolResultDownloads ToolResultDownloadsConfig
}

// ToolResultDownloadsConfig configures the Vertex tool-result file downloader.
type ToolResultDownloadsConfig struct {
	// MaxBytes is the maximum size in bytes for each downloaded file.
	// Defaults to 7 MiB (TS toolResultDownloads.maxBytes default) when zero.
	MaxBytes int64
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

	explicitBaseURL := cfg.BaseURL != ""
	baseURL := cfg.BaseURL
	var endpointBaseURL string
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	httpClient := cfg.HTTPClient

	if cfg.APIKey != "" {
		// Vertex express mode (TS parity): API key auth and publishers/google base URL.
		// Tuned "endpoints/{id}" models are rejected in Express Mode (checked
		// in LanguageModel), so no endpoint-style base URL is needed here.
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
			// Location is interpolated directly into the request host
			// (https://{location}-aiplatform.googleapis.com/...), so only a
			// single DNS label is accepted; a value like
			// "user@internal:8080/#" could otherwise rewrite the request
			// destination. Only validated here (not unconditionally above)
			// because an explicit BaseURL makes location unused. Ports TS
			// google-vertex-provider-base.ts (TS #21842).
			if !providerutils.IsValidHostnamePart(cfg.Location) {
				return nil, &providererrors.InvalidArgumentError{
					Field:   "location",
					Message: "Invalid Google Vertex location. Expected a single DNS label (letters, digits, and hyphens). Use `BaseURL` for custom endpoints.",
				}
			}
			baseURL = fmt.Sprintf("https://%s/v1beta1/projects/%s/locations/%s/publishers/google",
				vertexHost(cfg.Location), cfg.Project, cfg.Location)
			// Tuned models are addressed via their deployed endpoint
			// ".../locations/{region}/endpoints/{id}" instead of the
			// base-model ".../publishers/google/models/{id}" path, so they
			// omit the "/publishers/google" suffix (TS loadBaseURL({endpoint: true})).
			endpointBaseURL = fmt.Sprintf("https://%s/v1beta1/projects/%s/locations/%s",
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
		if httpClient != nil && httpClient.Transport != nil {
			baseTransport = httpClient.Transport
		}
		httpClient = &stdhttp.Client{
			Transport: &vertexinternal.AuthTransport{
				Base: baseTransport,
				TokenFunc: func(ctx context.Context) (string, error) {
					// Token override bypasses cached credentials.
					if cfg.AuthToken != nil {
						return cfg.AuthToken(ctx)
					}
					return tokenCache.tokenFor(ctx)
				},
			},
		}
	}

	mergedHeaders := version.WithUserAgentSuffix(http.MergeHeaders(headers, cfg.Headers), version.ProviderUserAgent("google-vertex"))
	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    mergedHeaders,
		HTTPClient: httpClient,
	})

	var endpointClient *http.Client
	// An explicit BaseURL is used verbatim for every model (TS: loadBaseURL
	// returns `options.baseURL` unconditionally when set, ignoring the
	// endpoint flag), so the alternate client only exists for auto-derived URLs.
	if !explicitBaseURL && endpointBaseURL != "" {
		endpointClient = http.NewClient(http.Config{
			BaseURL:    endpointBaseURL,
			Headers:    mergedHeaders,
			HTTPClient: httpClient,
		})
	}

	var cloudTTSClient *http.Client
	// Chirp speech models reject Express Mode outright (checked in
	// SpeechModel), so the Cloud TTS client is only built for OAuth auth.
	if cfg.APIKey == "" {
		cloudTTSBaseURL := cfg.CloudTTSBaseURL
		if cloudTTSBaseURL == "" {
			cloudTTSBaseURL = defaultCloudTTSSynthesizeURL
		}
		cloudTTSClient = http.NewClient(http.Config{
			BaseURL:    cloudTTSBaseURL,
			Headers:    mergedHeaders,
			HTTPClient: httpClient,
		})
	}

	return &Provider{
		config:         cfg,
		client:         client,
		endpointClient: endpointClient,
		cloudTTSClient: cloudTTSClient,
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
	if isEndpointModelID(modelID) && p.config.APIKey != "" {
		return nil, fmt.Errorf("Google Vertex tuned models do not support Express Mode API keys. Use standard Google Cloud credentials instead.") //nolint:staticcheck // matches TS SDK's exact error text
	}

	return NewLanguageModel(p, modelID), nil
}

// Interactions returns a language model backed by the Gemini Interactions
// API (`.../locations/{region}/interactions`) on Vertex. It reuses the base
// google package's InteractionsLanguageModel (TS: the exact same
// GoogleInteractionsLanguageModel class, just constructed with a
// Vertex-flavored config) with Vertex's OAuth-authenticated client and
// location-scoped, endpoint-style base URL (no "/publishers/google" suffix).
func (p *Provider) Interactions(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	if p.config.APIKey != "" {
		return nil, fmt.Errorf("Google Vertex Interactions models do not support Express Mode API keys. Use standard Google Cloud credentials instead.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return googleprovider.NewInteractionsLanguageModelWithConfig(p.interactionsConfig(), modelID), nil
}

// InteractionsAgent returns a Vertex Interactions API model for a Gemini
// agent preset (e.g. Deep Research).
func (p *Provider) InteractionsAgent(agent string) (provider.LanguageModel, error) {
	if agent == "" {
		return nil, fmt.Errorf("agent cannot be empty")
	}
	if p.config.APIKey != "" {
		return nil, fmt.Errorf("Google Vertex Interactions models do not support Express Mode API keys. Use standard Google Cloud credentials instead.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return googleprovider.NewInteractionsAgentModelWithConfig(p.interactionsConfig(), agent), nil
}

// InteractionsManagedAgent returns a Vertex Interactions API model for a
// user-defined agent created via the Agent Builder API.
func (p *Provider) InteractionsManagedAgent(id string) (provider.LanguageModel, error) {
	if id == "" {
		return nil, fmt.Errorf("managed agent id cannot be empty")
	}
	if p.config.APIKey != "" {
		return nil, fmt.Errorf("Google Vertex Interactions models do not support Express Mode API keys. Use standard Google Cloud credentials instead.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return googleprovider.NewInteractionsManagedAgentModelWithConfig(p.interactionsConfig(), id), nil
}

// interactionsConfig builds the InteractionsConfig for Vertex: the
// endpoint-style client when available (no "/publishers/google" suffix,
// matching TS `createConfig('interactions', { endpoint: true })`), falling
// back to the regular client when the caller supplied an explicit BaseURL
// (TS loadBaseURL returns an explicit baseURL verbatim regardless of the
// endpoint flag, so there is no separate endpoint client in that case).
func (p *Provider) interactionsConfig() googleprovider.InteractionsConfig {
	client := p.client
	if p.endpointClient != nil {
		client = p.endpointClient
	}
	return googleprovider.InteractionsConfig{
		ProviderName: "google.vertex.interactions",
		Client:       client,
		SerializableConfig: func() map[string]interface{} {
			return provider.SerializableConfig(p.config)
		},
	}
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
		AuthToken: p.vertexAuthToken,
	})
	return vertexAnthropic.LanguageModelWithOptions(modelID, opts)
}

// vertexAuthToken resolves a bearer token for any Vertex-authenticated
// sub-request using this provider's configured credentials (AuthToken,
// AccessToken, or TokenSource, in that order). Despite living on the path
// used to wire up the Anthropic sub-provider, it is provider-generic --
// also used by the Gemini live transcription WebSocket handshake (see
// gemini_transcription_stream.go) -- since Vertex's OAuth Bearer auth is
// shared across every model family, not specific to Anthropic.
func (p *Provider) vertexAuthToken(ctx context.Context) (string, error) {
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
	// Chirp 3: HD voices are served by the dedicated Cloud Text-to-Speech
	// API, not Vertex's generateContent endpoint (TS:
	// `modelId.startsWith('chirp')`).
	if strings.HasPrefix(modelID, "chirp") {
		if p.config.APIKey != "" {
			return nil, fmt.Errorf("Google Vertex Chirp speech models do not support Express Mode API keys. Use standard Google Cloud credentials instead.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		return NewCloudTTSSpeechModel(p, modelID), nil
	}
	return googleprovider.NewSpeechModelWithConfig(modelID, googleprovider.SpeechModelConfig{
		ProviderName:        "google.vertex.speech",
		MetadataKey:         "google",
		ProviderOptionsKeys: []string{"googleVertex", "vertex", "google"},
		GeneratePath: func(id string) string {
			return fmt.Sprintf("/models/%s:generateContent", id)
		},
		Client: p.client,
		// Kind/SerializableConfig let Serialize()/deserializeSpeechModel
		// (serialization.go) tell this Gemini TTS model apart from
		// CloudTTSSpeechModel, which shares the same "google.vertex.speech"
		// Provider() tag (see SER2 notes on SpeechModelConfig.Kind).
		Kind: "gemini-tts",
		SerializableConfig: func() map[string]interface{} {
			return provider.SerializableConfig(p.config)
		},
	}), nil
}

// Speech returns a speech synthesis model by ID (Gemini TTS via
// generateContent, or Chirp 3: HD via Cloud Text-to-Speech).
func (p *Provider) Speech(modelID string) (provider.SpeechModel, error) {
	return p.SpeechModel(modelID)
}

// TranscriptionModel returns a speech-to-text model by ID.
//
// Gemini-family model IDs (e.g. "gemini-3.5-transcribe",
// "gemini-3.5-transcribe-live") route to the generateContent/Live API
// surface via GeminiTranscriptionModel; everything else (Chirp, telephony)
// routes to Cloud Speech-to-Text v2 via TranscriptionModel. Mirrors TS
// createTranscriptionModel's `modelId.startsWith('gemini')` routing
// predicate exactly.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if p.config.APIKey != "" {
		return nil, fmt.Errorf("Google Vertex transcription models do not support Express Mode API keys. Use standard Google Cloud credentials instead.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if strings.HasPrefix(modelID, "gemini") {
		return NewGeminiTranscriptionModel(p, modelID), nil
	}
	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Google Vertex AI does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewVideoModel(p, modelID), nil
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
