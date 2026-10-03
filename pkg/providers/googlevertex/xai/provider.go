// Package xai implements the Go equivalent of TS's
// `@ai-sdk/google-vertex/xai` sub-package: Grok models served through Google
// Vertex AI's Model-as-a-Service (MaaS) OpenAI-compatible endpoint
// (https://cloud.google.com/vertex-ai/generative-ai/docs/partner-models/grok).
//
// TS builds this on `@ai-sdk/openai-compatible`'s createOpenAICompatible,
// pointed at Vertex's `.../endpoints/openapi` path -- entirely independent of
// `@ai-sdk/xai`'s own model implementation (which, as of the xAI Chat
// Completions removal this SDK already ported -- see
// pkg/providers/xai/provider.go -- now only implements the Responses API,
// which Vertex's Grok MaaS endpoint does not speak). This package mirrors
// that same independence: it builds directly on the shared OpenAI-compatible
// building blocks (pkg/providerutils/streaming.OpenAICompatStream,
// pkg/providerutils's request/response helpers) rather than on
// pkg/providers/xai, matching what TS actually reuses
// (`@ai-sdk/openai-compatible`, not `@ai-sdk/xai`) rather than what an
// earlier draft of this port assumed.
//
// TS's createGoogleVertex and createGoogleVertexXai are also independent
// top-level factories (`@ai-sdk/google-vertex` vs
// `@ai-sdk/google-vertex/xai`), not a sub-provider reachable from the main
// Vertex provider object -- so this package is standalone rather than a
// method on pkg/providers/googlevertex.Provider, with its own Google Cloud
// auth (falling back to Application Default Credentials, like the TS
// package's own auth module) instead of depending on that package's
// unexported provider type or ADC-fallback logic. The one piece of auth
// plumbing that is identical either way -- an http.RoundTripper that sets
// the Authorization Bearer header -- is a shared, provider-agnostic helper
// imported from pkg/providers/googlevertex/internal (a leaf package neither
// this package nor the parent googlevertex package needs to avoid), rather
// than duplicated here.

package xai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	vertexinternal "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/internal"
	"golang.org/x/oauth2"
)

// Config contains configuration for the Google Vertex xAI (Grok) sub-provider.
// Mirrors TS GoogleVertexXaiProviderSettings, plus the node/edge wrappers'
// auth options (GoogleAuthOptions / GoogleCredentials) folded in as
// AccessToken/AuthToken/TokenSource, since Go has no separate edge/node
// runtime split.
type Config struct {
	// Project is the Google Cloud project ID. Required.
	Project string

	// Location is the Google Cloud location/region. Defaults to "global".
	// Use "global" for the global endpoint.
	Location string

	// BaseURL overrides the computed Vertex MaaS OpenAPI endpoint
	// ("https://aiplatform.googleapis.com/v1/projects/{project}/locations/{location}/endpoints/openapi").
	BaseURL string

	// Headers are additional HTTP headers to include in every request.
	Headers map[string]string

	// AccessToken is a static OAuth2 Bearer token. Takes precedence over
	// TokenSource; ignored when AuthToken is set.
	AccessToken string

	// AuthToken resolves a bearer token per request. Overrides AccessToken
	// and TokenSource. Use this for custom/short-lived-credential flows.
	AuthToken func(ctx context.Context) (string, error) `json:"-"`

	// TokenSource provides OAuth2 tokens (e.g. from golang.org/x/oauth2/google).
	// Used when AuthToken and AccessToken are not set.
	TokenSource oauth2.TokenSource `json:"-"`

	// HTTPClient overrides the base HTTP transport (its own Transport, if
	// set, is wrapped with the auth transport rather than replaced).
	HTTPClient *http.Client `json:"-"`
}

// Provider implements provider.Provider for Grok models served through
// Google Vertex AI's MaaS OpenAI-compatible endpoint. Mirrors TS
// createGoogleVertexXai / GoogleVertexXaiProvider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

var _ provider.Provider = (*Provider)(nil)

// New creates a new Google Vertex xAI provider.
//
// Base URL construction mirrors TS's `constructBaseURL`:
//
//	https://aiplatform.googleapis.com/v1/projects/{project}/locations/{location}/endpoints/openapi
//
// with location defaulting to "global" when unset.
func New(cfg Config) (*Provider, error) {
	if cfg.Project == "" {
		return nil, fmt.Errorf("project is required for Google Vertex xAI")
	}

	location := cfg.Location
	if location == "" {
		location = "global"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf(
			"https://aiplatform.googleapis.com/v1/projects/%s/locations/%s/endpoints/openapi",
			cfg.Project, location,
		)
	}

	baseTransport := http.RoundTripper(http.DefaultTransport)
	var timeout time.Duration
	if cfg.HTTPClient != nil {
		if cfg.HTTPClient.Transport != nil {
			baseTransport = cfg.HTTPClient.Transport
		}
		timeout = cfg.HTTPClient.Timeout
	}
	authedClient := &http.Client{
		Timeout:   timeout,
		Transport: &vertexinternal.AuthTransport{Base: baseTransport, TokenFunc: cfg.resolveAuthToken()},
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		Headers:    internalhttp.MergeHeaders(map[string]string{"Content-Type": "application/json"}, cfg.Headers),
		HTTPClient: authedClient,
	})

	return &Provider{
		config: Config{Project: cfg.Project, Location: location, BaseURL: baseURL, Headers: cfg.Headers},
		client: client,
	}, nil
}

// Name returns the provider name. Matches TS's `name: 'googleVertex.xai'`
// passed to createOpenAICompatible.
func (p *Provider) Name() string {
	return "googleVertex.xai"
}

// newChatModel builds the Vertex-wrapped OpenAI-compatible Chat Completions
// model for modelID. Mirrors TS's shared `createChatModel`, used by both
// `languageModel` and `chatModel`.
func (p *Provider) newChatModel(modelID string) (*LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return &LanguageModel{provider: p, modelID: modelID}, nil
}

// LanguageModel returns a Grok language model by ID, served through Vertex
// MaaS. Mirrors TS provider.languageModel / the callable provider function.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.newChatModel(modelID)
}

// ChatModel returns a Grok chat model by ID. Mirrors TS provider.chatModel.
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.newChatModel(modelID)
}

// EmbeddingModel always errors: Vertex xAI Grok models do not support
// embeddings. Mirrors TS provider.embeddingModel/.textEmbeddingModel, which
// throw NoSuchModelError.
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "embeddingModel"}
}

// ImageModel always errors: Vertex xAI Grok models do not support image
// generation. Mirrors TS provider.imageModel, which throws NoSuchModelError.
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "imageModel"}
}

// SpeechModel always errors: not exposed by this sub-provider (TS
// GoogleVertexXaiProvider has no speech factory).
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("google-vertex xai does not support speech synthesis")
}

// TranscriptionModel always errors: not exposed by this sub-provider.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("google-vertex xai does not support transcription")
}

// RerankingModel always errors: not exposed by this sub-provider.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("google-vertex xai does not support reranking")
}

// VideoModel always errors: not exposed by this sub-provider.
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	return nil, fmt.Errorf("google-vertex xai does not support video generation")
}
