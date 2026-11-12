package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/verceloidc"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway/tools"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const (
	// DefaultBaseURL is the default AI Gateway API base URL
	DefaultBaseURL = "https://ai-gateway.vercel.sh/v4/ai"

	// AIGatewayProtocolVersion is the protocol version for the AI Gateway
	AIGatewayProtocolVersion = "0.0.1"

	// DefaultMetadataCacheRefresh is the default time to refresh metadata cache (5 minutes)
	DefaultMetadataCacheRefresh = 5 * time.Minute
)

var knownGatewayModelTypes = map[string]struct{}{
	"decision":  {},
	"embedding": {},
	// Keep "evaluation" until Gateway migrates its model catalog to "decision".
	"evaluation":    {},
	"image":         {},
	"language":      {},
	"realtime":      {},
	"reranking":     {},
	"speech":        {},
	"transcription": {},
	"video":         {},
}

// Provider implements the provider.Provider interface for AI Gateway
type Provider struct {
	config           Config
	client           *internalhttp.Client
	baseURL          string
	headers          map[string]string
	authResolver     gatewayAuthResolver
	metadataCache    *MetadataResponse
	metadataMutex    sync.RWMutex
	lastFetchTime    time.Time
	pendingMetadata  *metadataFetch
	cacheRefreshTime time.Duration
	nowFn            func() time.Time
}

// metadataFetch is an in-flight (or settled) metadata refresh shared by callers.
type metadataFetch struct {
	done chan struct{}
	meta *MetadataResponse
	err  error
}

type gatewayAuthResolver func(ctx context.Context) (token string, authMethod string, err error)

type gatewayAuthTransport struct {
	base     http.RoundTripper
	resolver gatewayAuthResolver
	// custom is set when the caller's HTTPClient brings its own
	// RoundTripper (the TS `fetch` option); see customFetchError.
	custom bool
}

func (t *gatewayAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.roundTrip(req)
	// A RoundTripper closes the request body, even on errors (a
	// credential or signing failure returns before the base transport).
	if err != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	return resp, err
}

func (t *gatewayAuthTransport) roundTrip(req *http.Request) (*http.Response, error) {
	// A redirect hop to another origin gets no credentials: the transport
	// runs once per hop, while TS adds the headers once per call and fetch
	// drops Authorization on a cross-origin redirect (credential transport
	// class sweep, S2).
	if providerutils.IsCrossOriginRedirectHop(req) {
		resp, err := t.base.RoundTrip(req.Clone(req.Context()))
		if err != nil && t.custom {
			return nil, customFetchError(err)
		}
		return resp, err
	}
	token, authMethod, err := t.resolver(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	// TS spreads options.headers after the auth headers, so a caller-supplied
	// Authorization or ai-gateway-auth-method header wins over the resolved
	// credential.
	if clone.Header.Get("Authorization") == "" {
		clone.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	if clone.Header.Get("ai-gateway-auth-method") == "" {
		clone.Header.Set("ai-gateway-auth-method", authMethod)
	}
	resp, err := t.base.RoundTrip(clone)
	if err != nil && t.custom {
		return nil, customFetchError(err)
	}
	return resp, err
}

func newGatewayHTTPClient(baseClient *http.Client, resolver gatewayAuthResolver) *http.Client {
	var httpClient *http.Client
	if baseClient != nil {
		clone := *baseClient
		httpClient = &clone
	} else {
		httpClient = &http.Client{}
	}
	baseTransport := http.RoundTripper(http.DefaultTransport)
	custom := false
	if httpClient.Transport != nil {
		baseTransport = httpClient.Transport
		_, std := baseTransport.(*http.Transport)
		custom = !std
	}
	if transport, ok := baseTransport.(*http.Transport); ok {
		baseTransport = transport.Clone()
	}
	httpClient.Transport = &gatewayAuthTransport{
		base:     baseTransport,
		resolver: resolver,
		custom:   custom,
	}
	return httpClient
}

func gatewayRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if requestID, ok := ctx.Value("x-vercel-id").(string); ok {
		return requestID
	}
	if requestID, ok := ctx.Value("X-Vercel-Id").(string); ok {
		return requestID
	}
	return ""
}

// Config contains configuration for the AI Gateway provider
type Config struct {
	// BatchResultDownloads configures JSON Lines batch result downloads, e.g.
	// the maximum bytes per result row (TS `batchResultDownloads`).
	BatchResultDownloads providerutils.BatchResultDownloads

	// APIKey is the AI Gateway API key
	// or Vercel access token. Can also be set via AI_GATEWAY_API_KEY.
	//
	// If neither APIKey nor AI_GATEWAY_API_KEY is a non-empty value, the
	// provider authenticates with the Vercel OIDC token instead, resolved for
	// each request (OIDCTokenFunc, else the ContextWithVercelOIDCToken token,
	// else VERCEL_OIDC_TOKEN). On Vercel
	// deployments this needs no configuration; locally, run `vercel env pull`
	// or `vercel dev`. A non-empty configured key is always used instead of
	// OIDC, even if it is invalid. With no OIDC token either, requests fail
	// with a GatewayAuthenticationError ("No authentication provided").
	APIKey string

	// APIKeyFunc resolves the API key or Vercel access token before each
	// request (TS apiKey given as a function, 7716e189ee). It must return a
	// non-empty key: an empty one (or an error) fails the request with a
	// GatewayAuthenticationError whose cause is a LoadAPIKeyError "AI Gateway
	// API key must be a non-empty string." (or the error). It wins over
	// AI_GATEWAY_API_KEY and OIDC and cannot be combined with APIKey.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

	// TeamIDOrSlug scopes Vercel access-token requests to a team.
	TeamIDOrSlug string

	// OIDCTokenFunc resolves the Vercel OIDC token used when no API key is
	// configured (TS getGatewayAuthToken's getVercelOidcToken()). Use
	// VercelOIDCTokenFunc for the TS lookup with token refresh. When nil, the
	// request context token (ContextWithVercelOIDCToken), else
	// VERCEL_OIDC_TOKEN, is read as is. An error (or an empty token) fails the
	// request with a GatewayAuthenticationError whose cause is that error.
	OIDCTokenFunc func(ctx context.Context) (string, error) `json:"-"`

	// BaseURL is the base URL for the AI Gateway API
	// Default: https://ai-gateway.vercel.sh/v4/ai
	BaseURL string

	// Headers are custom headers to include in requests
	Headers map[string]string `json:"headers,omitempty"`

	// MetadataCacheRefreshMillis is how frequently to refresh the metadata cache in milliseconds
	// Default: 300000 (5 minutes)
	MetadataCacheRefreshMillis int64

	// HTTPClient is a custom HTTP client to use for requests
	HTTPClient *http.Client

	// ZeroDataRetention enables zero data retention mode
	// When true, requests are not logged or retained by the gateway
	ZeroDataRetention bool

	// ProjectID is an optional project identifier for observability and billing attribution.
	// When set, it is forwarded as the "ai-o11y-project-id" header on all requests.
	// Can also be set via the VERCEL_PROJECT_ID environment variable.
	ProjectID *string

	// DisallowPromptTraining filters routing to providers that do not train on
	// prompt data. It is forwarded as providerOptions.gateway.disallowPromptTraining.
	DisallowPromptTraining bool

	// QuotaEntityID identifies the entity against which quota is tracked. It is
	// forwarded as providerOptions.gateway.quotaEntityId.
	QuotaEntityID string
}

// GatewayProviderOptions contains AI Gateway request-scoped routing,
// compliance, quota, and BYOK settings.
type GatewayProviderOptions struct {
	Only  []string `json:"only,omitempty"`
	Order []string `json:"order,omitempty"`
	Sort  string   `json:"sort,omitempty"`
	User  string   `json:"user,omitempty"`
	Tags  []string `json:"tags,omitempty"`

	// Models is the ordered list of fallback models to try. On evaluation
	// requests, the first entry may be a conditional GatewayModelFallback
	// (built with GatewayConditionalModelFallback) that reruns the
	// evaluation against a different model when its When condition matches
	// the primary answers; every other entry (and every entry for
	// non-evaluation requests) must be a plain model ID (built with
	// GatewayModel).
	Models                 []GatewayModelFallback      `json:"models,omitempty"`
	BYOK                   map[string][]map[string]any `json:"byok,omitempty"`
	ZeroDataRetention      *bool                       `json:"zeroDataRetention,omitempty"`
	DisallowPromptTraining *bool                       `json:"disallowPromptTraining,omitempty"`
	// Headquarters routes only to providers headquartered in these countries
	// (ISO 3166-1 alpha-2 codes, e.g. []string{"US", "DE"}), or prefix every
	// code with "!" to exclude countries instead (e.g. []string{"!US"}). A
	// list can't mix the two.
	Headquarters     []string                       `json:"headquarters,omitempty"`
	QuotaEntityID    string                         `json:"quotaEntityId,omitempty"`
	ProviderTimeouts *GatewayProviderTimeoutOptions `json:"providerTimeouts,omitempty"`
	ServiceTier      string                         `json:"serviceTier,omitempty"`

	// Has restricts routing to provider models that satisfy every given
	// entry. Entries are capability tags (GatewayHasImplicitCaching,
	// GatewayHasReasoning, GatewayHasStructuredOutput, GatewayHasToolUse,
	// GatewayHasVision) or weight-format conditions built with
	// GatewayHasQuantization / GatewayHasNotQuantization.
	Has []string `json:"has,omitempty"`

	// IdempotencyKey is used by experimental_startBatch: retries with the
	// same key replay the original batch instead of creating a duplicate.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`

	// Caching enables automatic caching behavior when supported by the
	// Gateway. The only valid value is GatewayCachingAuto ("auto").
	Caching string `json:"caching,omitempty"`

	// ExperimentalStreamingImageDecoding (experimental, image models only)
	// decodes large base64 images while the response is read, to reduce peak
	// memory (TS experimental_streamingImageDecoding, 89aff56165). Extracted
	// images are returned as bytes (ImageResult.Images, with Base64Images
	// empty); if a result mixes extracted and small images, all are returned
	// as bytes. Defaults to false. Image models remove this client option
	// from requests.
	ExperimentalStreamingImageDecoding *bool `json:"experimental_streamingImageDecoding,omitempty"`
}

// Gateway `has` capability tags. See GatewayProviderOptions.Has.
const (
	GatewayHasImplicitCaching  = "implicit-caching"
	GatewayHasReasoning        = "reasoning"
	GatewayHasStructuredOutput = "structured-output"
	GatewayHasToolUse          = "tool-use"
	GatewayHasVision           = "vision"
)

// GatewayCachingAuto is the only valid value for GatewayProviderOptions.Caching.
const GatewayCachingAuto = "auto"

// GatewayHasQuantization returns a `has` entry that requires the serving
// provider to report the given weight format (e.g. "fp8").
func GatewayHasQuantization(format string) string {
	return "quantization:" + format
}

// GatewayHasNotQuantization returns a `has` entry that excludes the given
// weight format. Providers with no recorded format still pass this exclusion.
func GatewayHasNotQuantization(format string) string {
	return "!quantization:" + format
}

// GatewayProviderTimeoutOptions contains Gateway provider timeout settings.
type GatewayProviderTimeoutOptions struct {
	BYOK map[string]int `json:"byok,omitempty"`
}

// ToProviderOptions returns a GenerateOptions.ProviderOptions map containing
// these Gateway options under the "gateway" key.
func (o GatewayProviderOptions) ToProviderOptions() map[string]interface{} {
	return map[string]interface{}{"gateway": o.toMap()}
}

func (o GatewayProviderOptions) toMap() map[string]interface{} {
	out := map[string]interface{}{}
	if len(o.Only) > 0 {
		out["only"] = o.Only
	}
	if len(o.Order) > 0 {
		out["order"] = o.Order
	}
	if o.Sort != "" {
		out["sort"] = o.Sort
	}
	if o.User != "" {
		out["user"] = o.User
	}
	if len(o.Tags) > 0 {
		out["tags"] = o.Tags
	}
	if len(o.Models) > 0 {
		models := make([]interface{}, len(o.Models))
		for i, m := range o.Models {
			models[i] = m.toWire()
		}
		out["models"] = models
	}
	if len(o.BYOK) > 0 {
		out["byok"] = o.BYOK
	}
	if o.ZeroDataRetention != nil {
		out["zeroDataRetention"] = *o.ZeroDataRetention
	}
	if o.DisallowPromptTraining != nil {
		out["disallowPromptTraining"] = *o.DisallowPromptTraining
	}
	if len(o.Headquarters) > 0 {
		out["headquarters"] = o.Headquarters
	}
	if o.QuotaEntityID != "" {
		out["quotaEntityId"] = o.QuotaEntityID
	}
	if o.ProviderTimeouts != nil && len(o.ProviderTimeouts.BYOK) > 0 {
		out["providerTimeouts"] = map[string]interface{}{"byok": o.ProviderTimeouts.BYOK}
	}
	if o.ServiceTier != "" {
		out["serviceTier"] = o.ServiceTier
	}
	if len(o.Has) > 0 {
		out["has"] = o.Has
	}
	if o.IdempotencyKey != "" {
		out["idempotencyKey"] = o.IdempotencyKey
	}
	if o.Caching != "" {
		out["caching"] = o.Caching
	}
	if o.ExperimentalStreamingImageDecoding != nil {
		out[gatewayStreamingImageDecodingKey] = *o.ExperimentalStreamingImageDecoding
	}
	return out
}

// WithProjectID returns a Config option that sets the project ID for observability.
// The project ID is forwarded as the "ai-o11y-project-id" header on all gateway requests.
func WithProjectID(id string) func(*Config) {
	return func(c *Config) {
		c.ProjectID = &id
	}
}

// MetadataResponse contains available models and providers from the gateway
type MetadataResponse struct {
	Models []ModelMetadata `json:"models"`
}

// ModelMetadata contains information about a model
type ModelMetadata struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Description   *string            `json:"description,omitempty"`
	Pricing       *ModelPricing      `json:"pricing,omitempty"`
	Specification ModelSpecification `json:"specification"`
	ModelType     string             `json:"modelType,omitempty"`
}

type ModelPricing struct {
	Input                    string `json:"input"`
	Output                   string `json:"output"`
	CachedInputTokens        string `json:"cachedInputTokens,omitempty"`
	CacheCreationInputTokens string `json:"cacheCreationInputTokens,omitempty"`
}

type ModelSpecification struct {
	SpecificationVersion string `json:"specificationVersion"`
	Provider             string `json:"provider"`
	ModelID              string `json:"modelId"`
}

type metadataResponseWire struct {
	Models []modelMetadataWire `json:"models"`
}

type modelMetadataWire struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Description   *string                `json:"description"`
	Pricing       *modelPricingWire      `json:"pricing"`
	Specification modelSpecificationWire `json:"specification"`
	// ModelType is a pointer: TS keeps only a null/absent or known
	// modelType, so an empty string is filtered out like any unknown type.
	ModelType *string `json:"modelType"`
}

type modelPricingWire struct {
	Input           string `json:"input"`
	Output          string `json:"output"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

type modelSpecificationWire struct {
	SpecificationVersion string `json:"specificationVersion"`
	Provider             string `json:"provider"`
	ModelID              string `json:"modelId"`
}

// CreditsInfo contains credit information for the authenticated user
type CreditsInfo struct {
	Balance   string `json:"balance"`
	TotalUsed string `json:"totalUsed"`
}

// New creates a new AI Gateway provider with the given configuration.
// Optional functional options (e.g. WithProjectID) can be passed to override config fields.
func New(cfg Config, opts ...func(*Config)) (*Provider, error) {
	// Apply functional options
	for _, opt := range opts {
		opt(&cfg)
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = providerutils.WithoutTrailingSlash(baseURL)

	authResolver := func(ctx context.Context) (string, string, error) {
		return resolveGatewayAuthToken(ctx, cfg)
	}

	// Create headers with authentication
	headers := map[string]string{
		"ai-gateway-protocol-version": AIGatewayProtocolVersion,
	}

	// TS: the team header precedes options.headers, so a custom header of the
	// same name wins.
	if cfg.TeamIDOrSlug != "" {
		headers["x-vercel-ai-gateway-team"] = cfg.TeamIDOrSlug
	}

	// Add custom headers
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	// Add zero data retention header if enabled
	if cfg.ZeroDataRetention {
		headers["ai-gateway-zero-retention"] = "true"
	}

	// Inject project ID header when explicitly configured
	if cfg.ProjectID != nil {
		headers["ai-o11y-project-id"] = *cfg.ProjectID
	}
	headers = version.WithUserAgentSuffix(headers, version.ProviderUserAgent("gateway"))

	httpClient := newGatewayHTTPClient(cfg.HTTPClient, authResolver)

	// Create HTTP client
	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		Headers:    headers,
		HTTPClient: httpClient,
	})

	// Set cache refresh time
	cacheRefreshTime := DefaultMetadataCacheRefresh
	if cfg.MetadataCacheRefreshMillis > 0 {
		cacheRefreshTime = time.Duration(cfg.MetadataCacheRefreshMillis) * time.Millisecond
	}

	return &Provider{
		config:           cfg,
		client:           client,
		baseURL:          baseURL,
		headers:          headers,
		authResolver:     authResolver,
		cacheRefreshTime: cacheRefreshTime,
	}, nil
}

// CreateGateway creates a new AI Gateway provider.
//
// It mirrors the TypeScript SDK createGateway export while New remains the
// idiomatic Go constructor.
func CreateGateway(cfg Config, opts ...func(*Config)) (*Provider, error) {
	return New(cfg, opts...)
}

// CreateGatewayProvider creates a new AI Gateway provider.
//
// Deprecated: use CreateGateway.
func CreateGatewayProvider(cfg Config, opts ...func(*Config)) (*Provider, error) {
	return CreateGateway(cfg, opts...)
}

func resolveGatewayAuthToken(ctx context.Context, cfg Config) (token string, authMethod string, err error) {
	if cfg.APIKeyFunc != nil {
		key, err := providerutils.ResolveAPIKey(ctx, providerutils.ResolveAPIKeyOptions{
			APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, Description: "AI Gateway", RequireNonEmpty: true,
		})
		if err != nil {
			// TS getHeaders wraps every credential failure in the contextual
			// authentication error.
			return "", "", gatewayerrors.CreateContextualAuthenticationError(false, false, http.StatusUnauthorized, err, "")
		}
		return key, "api-key", nil
	}
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("AI_GATEWAY_API_KEY")
	}
	if apiKey != "" {
		return apiKey, "api-key", nil
	}

	if cfg.OIDCTokenFunc != nil {
		token, err := cfg.OIDCTokenFunc(ctx)
		if err == nil && token == "" {
			err = &VercelOIDCTokenError{Message: vercelOIDCMissingMessage}
		}
		if err != nil {
			return "", "", gatewayerrors.CreateContextualAuthenticationError(false, false, http.StatusUnauthorized, err, "")
		}
		return token, "oidc", nil
	}

	// TS getVercelOidcToken reads the request context token
	// (ContextWithVercelOIDCToken) before VERCEL_OIDC_TOKEN. Without
	// OIDCTokenFunc neither is refreshed (owner decision b).
	if oidcToken := verceloidc.TokenFromContext(ctx); oidcToken != "" {
		return oidcToken, "oidc", nil
	}
	if oidcToken := strings.TrimSpace(os.Getenv("VERCEL_OIDC_TOKEN")); oidcToken != "" {
		return oidcToken, "oidc", nil
	}

	return "", "", gatewayerrors.CreateContextualAuthenticationError(false, false, http.StatusUnauthorized, fmt.Errorf("no API key or OIDC token configured"), "")
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "gateway"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
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
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewImageModel(p, modelID), nil
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewVideoModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewSpeechModel(p, modelID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewRerankingModel(p, modelID), nil
}

// Reranking returns a reranking model by ID.
//
// It mirrors the TypeScript SDK reranking alias for rerankingModel.
func (p *Provider) Reranking(modelID string) (provider.RerankingModel, error) {
	return p.RerankingModel(modelID)
}

// GetAvailableModels returns available providers and models from the gateway.
//
// Mirrors TS getAvailableModels: once the refresh window has elapsed a refresh
// is started in the background, but an existing (stale) cache is still returned
// immediately; only the first call waits for the fetch.
func (p *Provider) GetAvailableModels(ctx context.Context) (*MetadataResponse, error) {
	p.metadataMutex.Lock()
	now := p.now()
	if p.pendingMetadata == nil || now.Sub(p.lastFetchTime) > p.cacheRefreshTime {
		p.lastFetchTime = now
		f := &metadataFetch{done: make(chan struct{})}
		p.pendingMetadata = f
		go func() {
			defer close(f.done)
			f.meta, f.err = p.fetchMetadata(context.WithoutCancel(ctx))
			if f.err == nil {
				p.metadataMutex.Lock()
				p.metadataCache = f.meta
				p.metadataMutex.Unlock()
			}
		}()
	}
	cached := p.metadataCache
	pending := p.pendingMetadata
	p.metadataMutex.Unlock()
	if cached != nil {
		return cached, nil
	}
	select {
	case <-pending.done:
		return pending.meta, pending.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Provider) now() time.Time {
	if p.nowFn != nil {
		return p.nowFn()
	}
	return time.Now()
}

func (p *Provider) fetchMetadata(ctx context.Context) (*MetadataResponse, error) {
	// Fetch fresh metadata
	resp, err := p.client.Get(ctx, "/config")
	if err != nil {
		return nil, p.handleErrorWithContext(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, p.gatewayAPIErrorWithContext(ctx, resp)
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text() before JSON.parse
	// TS createJsonResponseHandler(gatewayAvailableModelsResponseSchema): a
	// body that is not JSON or fails the schema is the APICallError "Invalid
	// JSON response" with the response status, which asGatewayError turns
	// into a GatewayResponseError (gateway-fetch-metadata.ts:37).
	var wire metadataResponseWire
	if err := json.Unmarshal(resp.Body, &wire); err != nil {
		return nil, p.handleErrorWithContext(ctx, providerutils.InvalidJSONResponseError("gateway", resp.StatusCode, resp.Headers, resp.Body, nil))
	}
	if err := validateMetadataResponse(resp.Body); err != nil {
		return nil, p.handleErrorWithContext(ctx, providerutils.InvalidJSONResponseError("gateway", resp.StatusCode, resp.Headers, resp.Body, nil))
	}
	metadata := MetadataResponse{Models: make([]ModelMetadata, 0, len(wire.Models))}
	for _, model := range wire.Models {
		modelType := ""
		if model.ModelType != nil {
			if _, ok := knownGatewayModelTypes[*model.ModelType]; !ok {
				// TS parity: unknown modelType entries are filtered out instead of failing parsing.
				continue
			}
			modelType = *model.ModelType
		}
		var pricing *ModelPricing
		if model.Pricing != nil {
			pricing = &ModelPricing{
				Input:                    model.Pricing.Input,
				Output:                   model.Pricing.Output,
				CachedInputTokens:        model.Pricing.InputCacheRead,
				CacheCreationInputTokens: model.Pricing.InputCacheWrite,
			}
		}
		metadata.Models = append(metadata.Models, ModelMetadata{
			ID:          model.ID,
			Name:        model.Name,
			Description: model.Description,
			Pricing:     pricing,
			Specification: ModelSpecification{
				SpecificationVersion: model.Specification.SpecificationVersion,
				Provider:             model.Specification.Provider,
				ModelID:              model.Specification.ModelID,
			},
			ModelType: modelType,
		})
	}

	return &metadata, nil
}

// GetCredits returns credit information for the authenticated user.
//
// Unlike other Gateway endpoints, /v1/credits selects the team from a
// `teamId` or `slug` query parameter rather than the
// x-vercel-ai-gateway-team header, so forward the resolved team there too
// (TS getTeamIdOrSlug in gateway-fetch-metadata.ts). Without this, a
// credential that can access multiple teams (e.g. a Vercel access token)
// has no default team and the request is rejected with 401, even though
// the same team header works for model calls and getSpendReport.
func (p *Provider) GetCredits(ctx context.Context) (*CreditsInfo, error) {
	path := "/v1/credits"
	// TS getTeamIdOrSlug: value?.trim() || undefined, then
	// url.searchParams.set (URLSearchParams escaping).
	if teamIDOrSlug := providerutils.JSTrim(gatewayTeamFromHeaders(p.headers)); teamIDOrSlug != "" {
		key := "slug"
		if strings.HasPrefix(teamIDOrSlug, "team_") {
			key = "teamId"
		}
		path += "?" + internalhttp.EncodeQuery(map[string]string{key: teamIDOrSlug})
	}
	body, err := p.doOriginRequest(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := providerutils.MediaResponseValidator("gateway", providerutils.SchemaGatewayCredits)(http.StatusOK, http.Header{}, body); err != nil {
		return nil, p.handleErrorWithContext(ctx, err)
	}
	var wire struct {
		Balance   string `json:"balance"`
		TotalUsed string `json:"total_used"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, p.handleErrorWithContext(ctx, err)
	}
	return &CreditsInfo{
		Balance:   wire.Balance,
		TotalUsed: wire.TotalUsed,
	}, nil
}

func (p *Provider) originClient() (*internalhttp.Client, error) {
	base, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, err
	}
	return internalhttp.NewClient(internalhttp.Config{
		BaseURL:    base.Scheme + "://" + base.Host,
		Headers:    p.headers,
		HTTPClient: newGatewayHTTPClient(p.config.HTTPClient, p.authResolver),
	}), nil
}

func (p *Provider) handleErrorWithContext(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	// Caller cancellation is not a gateway failure (TS 9ca8bcf731).
	if isGatewayCallerCancellation(ctx, err) {
		return err
	}
	if gatewayerrors.IsTimeoutError(err) {
		return gatewayerrors.ConvertToGatewayTimeoutError(err, "gateway")
	}
	if ge, ok := gatewayErrorIn(err); ok {
		return ge
	}
	if converted, ok := p.gatewayInvalidJSONError(ctx, err); ok {
		return converted
	}
	if providererrors.IsProviderError(err) {
		return err
	}
	var httpStatusErr *internalhttp.HTTPStatusError
	if errors.As(err, &httpStatusErr) {
		return p.gatewayAPIErrorWithContext(ctx, &internalhttp.Response{
			StatusCode: httpStatusErr.StatusCode,
			Headers:    httpStatusErr.Headers,
			Body:       httpStatusErr.Body,
		})
	}
	return p.gatewayUnknownError(err)
}

func (p *Provider) doOriginRequest(ctx context.Context, path string) ([]byte, error) {
	client, err := p.originClient()
	if err != nil {
		return nil, p.handleErrorWithContext(ctx, err)
	}

	resp, err := client.Get(ctx, path)
	if err != nil {
		return nil, p.handleErrorWithContext(ctx, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, p.gatewayAPIErrorWithContext(ctx, resp)
	}
	return []byte(textdecode.UTF8(resp.Body)), nil // TS response.text() before JSON.parse
}

func (p *Provider) gatewayAPIErrorWithAuthMethod(resp *internalhttp.Response, authMethod string) error {
	if resp == nil {
		return gatewayerrors.NewGatewayResponseError("Gateway request failed", 0, nil, nil, nil, "")
	}
	// TS createJsonErrorResponseHandler reads response.text() (BOM dropped,
	// invalid UTF-8 replaced) before parsing (gateway-provider.ts:462).
	decoded := *resp
	decoded.Body = []byte(textdecode.UTF8(resp.Body))
	resp = &decoded
	cause := &providererrors.ProviderError{
		Provider:        "gateway",
		StatusCode:      resp.StatusCode,
		Message:         gatewayErrorMessage(resp.Body, resp.StatusCode),
		ResponseHeaders: providerutils.ExtractHeaders(resp.Headers),
		ResponseBody:    string(resp.Body),
		Data:            gatewayErrorData(resp.Body),
	}
	return gatewayerrors.CreateGatewayErrorFromResponse(resp.Body, resp.StatusCode, "Gateway request failed", cause, authMethod)
}

// gatewayErrorMessage mirrors TS provider-utils createJsonErrorResponseHandler
// + getErrorMessage (@ai-sdk/provider): an empty response body or one that
// fails to parse as JSON falls back to the HTTP status text (TS
// `response.statusText`); a JSON string body is used as-is; a JSON `null`
// body yields "unknown error" (TS getErrorMessage(null)); any other JSON
// value is re-serialized. Used to populate the nested cause's message with
// the full error body instead of the generic "Gateway request failed"
// placeholder.
func gatewayErrorMessage(body []byte, statusCode int) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return gatewayStatusTextFallback(statusCode)
	}
	var value interface{}
	if err := securejson.Unmarshal(body, &value); err != nil {
		return gatewayStatusTextFallback(statusCode)
	}
	if value == nil {
		return "unknown error"
	}
	if str, ok := value.(string); ok {
		return str
	}
	// TS JSON.stringify of the parsed body: JS key order, HTML characters
	// left unescaped.
	if ordered, ok := jsonorder.Parse(body); ok {
		return jsonorder.Stringify(ordered)
	}
	serialized, err := json.Marshal(value)
	if err != nil {
		return gatewayStatusTextFallback(statusCode)
	}
	return string(serialized)
}

// gatewayStatusTextFallback returns the standard HTTP status text for
// statusCode (TS `response.statusText`), or "unknown error" for codes with
// no standard text (e.g. 0, from a response the SDK never received).
func gatewayStatusTextFallback(statusCode int) string {
	if text := http.StatusText(statusCode); text != "" {
		return text
	}
	return "unknown error"
}

// gatewayErrorData best-effort decodes the response body into a generic
// value for ProviderError.Data, mirroring the parsed error body TS providers
// attach to their APICallError.data.
func gatewayErrorData(body []byte) interface{} {
	if len(body) == 0 {
		return nil
	}
	var value interface{}
	if err := securejson.Unmarshal(body, &value); err != nil {
		return nil
	}
	return value
}

func (p *Provider) gatewayAPIErrorWithContext(ctx context.Context, resp *internalhttp.Response) error {
	if resp == nil {
		return gatewayerrors.NewGatewayResponseError("Gateway request failed", 0, nil, nil, nil, "")
	}
	authMethod := p.requestAuthMethod(ctx)
	// TS createJsonErrorResponseHandler reads response.text() (BOM dropped,
	// invalid UTF-8 replaced).
	decoded := *resp
	decoded.Body = []byte(textdecode.UTF8(resp.Body))
	resp = &decoded
	cause := &providererrors.ProviderError{
		Provider:        "gateway",
		StatusCode:      resp.StatusCode,
		Message:         gatewayErrorMessage(resp.Body, resp.StatusCode),
		ResponseHeaders: providerutils.ExtractHeaders(resp.Headers),
		ResponseBody:    string(resp.Body),
		Data:            gatewayErrorData(resp.Body),
	}
	return gatewayerrors.CreateGatewayErrorFromResponse(resp.Body, resp.StatusCode, "Gateway request failed", cause, authMethod)
}

// gatewayInvalidJSONError converts a successful body that failed its response
// schema (TS createJsonResponseHandler APICallError "Invalid JSON response")
// the way TS asGatewayError does for any APICallError: the body is parsed as
// a gateway error response (which fails for a normal body, giving a
// GatewayResponseError "Invalid error response format: Gateway request
// failed" with the original status), with the APICallError as the cause.
func (p *Provider) gatewayInvalidJSONError(ctx context.Context, err error) (error, bool) {
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != providererrors.InvalidJSONResponseMessage {
		return nil, false
	}
	authMethod := p.requestAuthMethod(ctx)
	return gatewayerrors.CreateGatewayErrorFromResponse([]byte(pe.ResponseBody), pe.StatusCode, "Gateway request failed", pe, authMethod), true
}

// isGatewayCallerCancellation mirrors TS asGatewayError's first check (TS
// 9ca8bcf731): an AbortError or TimeoutError of the caller is rethrown
// unchanged instead of becoming a retryable 500. In Go that is a
// context.Canceled, or the error of the caller's ctx once it is done (its
// deadline included); a transport timeout while ctx is still live stays a
// GatewayTimeoutError.
func isGatewayCallerCancellation(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return ctx != nil && ctx.Err() != nil && errors.Is(err, ctx.Err())
}

func (p *Provider) gatewayUnknownError(err error) error {
	if err == nil {
		return nil
	}
	// TS parity: asGatewayError's fallback path (opaque, non-APICallError
	// errors) marks the resulting error's retryability explicitly rather than
	// relying purely on the (unknown, defaulted-to-500) status code. A JSON
	// decode failure is a permanent error (the body will never parse
	// differently on retry); a transient network/body-read error is
	// retryable. See 90192f1.
	if rejected, ok := internalhttp.FetchRejectionCause(err); ok {
		err = rejected // TS: `Gateway request failed: ${error.message}`
	} else if converted, ok := gatewayConnectError(err); ok {
		return converted
	}
	retryable := !isGatewayJSONDecodeError(err)
	return gatewayerrors.NewGatewayResponseErrorWithRetryable(
		fmt.Sprintf("Invalid error response format: Gateway request failed: %s", err.Error()),
		http.StatusInternalServerError,
		map[string]interface{}{},
		fmt.Errorf("invalid gateway error response"),
		err,
		"",
		&retryable,
	)
}

// isGatewayJSONDecodeError reports whether err originates from a JSON
// decode/parse failure (as opposed to a network or body-read error). Such
// errors are never retryable: the same malformed body will fail identically
// on retry.
//
// NOTE: recovering the *real* HTTP status code for a body-read failure that
// occurs after a successful (2xx) response — so it can be preserved instead
// of defaulted to 500, matching TS's `statusCode < 400` check in
// asGatewayError — requires internal/http/client.go to surface the status
// code on read errors from Do(). That is a core dependency outside this
// package's scope (see state/parity/sep_23_2026/gateway.md row 90192f1).
func isGatewayJSONDecodeError(err error) bool {
	if err == nil {
		return false
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	return strings.Contains(err.Error(), "failed to decode JSON response")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

// Tools returns gateway-specific provider-defined tools
type Tools struct{}

// ParallelSearch creates a parallel search tool with the given configuration
func (t *Tools) ParallelSearch(config tools.ParallelSearchConfig) tools.ParallelSearchTool {
	return tools.NewParallelSearch(config)
}

// PerplexitySearch creates a perplexity search tool with the given configuration
func (t *Tools) PerplexitySearch(config tools.PerplexitySearchConfig) tools.PerplexitySearchTool {
	return tools.NewPerplexitySearch(config)
}

// ExaSearch creates an Exa search tool with the given configuration.
func (t *Tools) ExaSearch(config tools.ExaSearchConfig) tools.ExaSearchTool {
	return tools.NewExaSearch(config)
}

// TakoSearch creates a Tako search tool with the given configuration.
func (t *Tools) TakoSearch(config tools.TakoSearchConfig) tools.TakoSearchTool {
	return tools.NewTakoSearch(config)
}

// BrowserbaseSearch creates a Browserbase search tool with the given
// configuration.
func (t *Tools) BrowserbaseSearch(config tools.BrowserbaseSearchConfig) tools.BrowserbaseSearchTool {
	return tools.NewBrowserbaseSearch(config)
}

// BrowserbaseFetch creates a Browserbase fetch tool with the given
// configuration.
func (t *Tools) BrowserbaseFetch(config tools.BrowserbaseFetchConfig) tools.BrowserbaseFetchTool {
	return tools.NewBrowserbaseFetch(config)
}

// DeepSeekSearch creates a DeepSeek search tool with the given configuration
// (TS gatewayTools.deepseekSearch, 2099d455e4).
func (t *Tools) DeepSeekSearch(config tools.DeepSeekSearchConfig) tools.DeepSeekSearchTool {
	return tools.NewDeepSeekSearch(config)
}

// DeepSeekFetch creates a DeepSeek fetch tool with the given configuration
// (TS gatewayTools.deepseekFetch, 2099d455e4).
func (t *Tools) DeepSeekFetch(config tools.DeepSeekFetchConfig) tools.DeepSeekFetchTool {
	return tools.NewDeepSeekFetch(config)
}

// NewTools creates a new Tools instance for accessing gateway-specific tools
func NewTools() *Tools {
	return &Tools{}
}

// Default gateway provider instance, guarded by defaultProviderMu so
// concurrent first calls to Gateway() build it once (TS creates the default
// `gateway` once at module load).
var (
	defaultProviderMu sync.Mutex
	defaultProvider   *Provider
)

// Gateway returns the default gateway provider
// It reads configuration from environment variables. It is safe for
// concurrent use; a failed construction is retried on the next call.
func Gateway() (*Provider, error) {
	defaultProviderMu.Lock()
	defer defaultProviderMu.Unlock()
	if defaultProvider == nil {
		p, err := New(Config{})
		if err != nil {
			return nil, err
		}
		defaultProvider = p
	}
	return defaultProvider, nil
}

// O11yHeaders contains observability headers for Vercel deployments
type O11yHeaders struct {
	DeploymentID string
	Environment  string
	Region       string
	RequestID    string
	// ProjectID is the Vercel project identifier, read from VERCEL_PROJECT_ID
	ProjectID string
}

// GetO11yHeaders returns observability headers from the environment
func GetO11yHeaders(ctx context.Context) O11yHeaders {
	return O11yHeaders{
		DeploymentID: os.Getenv("VERCEL_DEPLOYMENT_ID"),
		Environment:  os.Getenv("VERCEL_ENV"),
		Region:       os.Getenv("VERCEL_REGION"),
		RequestID:    gatewayRequestID(ctx),
		ProjectID:    os.Getenv("VERCEL_PROJECT_ID"),
	}
}

// gatewayRequestHeaders merges the per-call headers, the model's own headers
// and the observability headers in the TS order (later wins):
// combineHeaders(config, options.headers, model headers, o11y). The client's
// own headers are applied by the HTTP client underneath.
func gatewayRequestHeaders(ctx context.Context, call, model map[string]string) map[string]string {
	headers := internalhttp.MergeHeaders(call, model)
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	return headers
}

// AddO11yHeaders adds observability headers to the request headers
func AddO11yHeaders(headers map[string]string, o11y O11yHeaders) {
	if o11y.DeploymentID != "" {
		headers["ai-o11y-deployment-id"] = o11y.DeploymentID
	}
	if o11y.Environment != "" {
		headers["ai-o11y-environment"] = o11y.Environment
	}
	if o11y.Region != "" {
		headers["ai-o11y-region"] = o11y.Region
	}
	if o11y.RequestID != "" {
		headers["ai-o11y-request-id"] = o11y.RequestID
	}
	if o11y.ProjectID != "" {
		headers["ai-o11y-project-id"] = o11y.ProjectID
	}
}

// MarshalJSONField is a helper to marshal a field to JSON
func MarshalJSONField(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// validateMetadataResponse applies the required-field checks of TS
// gatewayAvailableModelsResponseSchema that the lenient wire struct cannot
// see: `models` must be an array and every model needs id, name and a
// specification of version "v4" with provider and modelId.
func validateMetadataResponse(body []byte) error {
	var raw struct {
		Models *[]struct {
			ID            *string `json:"id"`
			Name          *string `json:"name"`
			Specification *struct {
				SpecificationVersion *string `json:"specificationVersion"`
				Provider             *string `json:"provider"`
				ModelID              *string `json:"modelId"`
			} `json:"specification"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	if raw.Models == nil {
		return errors.New("Invalid JSON response: models must be an array") //nolint:staticcheck // mirrors TS wording
	}
	for i, m := range *raw.Models {
		spec := m.Specification
		if m.ID == nil || m.Name == nil || spec == nil || spec.Provider == nil || spec.ModelID == nil ||
			spec.SpecificationVersion == nil || *spec.SpecificationVersion != "v4" {
			return fmt.Errorf("Invalid JSON response: models[%d] does not match the gateway model schema", i) //nolint:staticcheck // mirrors TS wording
		}
	}
	return nil
}

// requestAuthMethod is the auth method of the headers a failed request was
// sent with (TS parseAuthMethod(resolvedHeaders)). A key func is not called
// again: its requests always use "api-key".
func (p *Provider) requestAuthMethod(ctx context.Context) string {
	if p.config.APIKeyFunc != nil {
		return "api-key"
	}
	if p.authResolver == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_, authMethod, _ := p.authResolver(ctx)
	return authMethod
}
