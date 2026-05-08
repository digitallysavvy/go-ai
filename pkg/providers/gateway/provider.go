package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway/tools"
)

const (
	// DefaultBaseURL is the default AI Gateway API base URL
	DefaultBaseURL = "https://ai-gateway.vercel.sh/v4/ai"

	// AIGatewayProtocolVersion is the protocol version for the AI Gateway
	AIGatewayProtocolVersion = "0.0.1"

	// DefaultMetadataCacheRefresh is the default time to refresh metadata cache (5 minutes)
	DefaultMetadataCacheRefresh = 5 * time.Minute
)

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
	pendingMetadata  *sync.Once
	cacheRefreshTime time.Duration
}

type gatewayAuthResolver func(ctx context.Context) (token string, authMethod string, err error)

type gatewayAuthTransport struct {
	base     http.RoundTripper
	resolver gatewayAuthResolver
}

func (t *gatewayAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, authMethod, err := t.resolver(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	clone.Header.Set("ai-gateway-auth-method", authMethod)
	return t.base.RoundTrip(clone)
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
	if httpClient.Transport != nil {
		baseTransport = httpClient.Transport
	}
	if transport, ok := baseTransport.(*http.Transport); ok {
		baseTransport = transport.Clone()
	}
	httpClient.Transport = &gatewayAuthTransport{
		base:     baseTransport,
		resolver: resolver,
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
	// APIKey is the AI Gateway API key
	// Can also be set via AI_GATEWAY_API_KEY environment variable
	APIKey string

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

	// HIPAACompliant filters routing to providers that are HIPAA compliant with
	// Vercel AI Gateway. It is forwarded as providerOptions.gateway.hipaaCompliant.
	HIPAACompliant bool

	// QuotaEntityID identifies the entity against which quota is tracked. It is
	// forwarded as providerOptions.gateway.quotaEntityId.
	QuotaEntityID string
}

// GatewayProviderOptions contains AI Gateway request-scoped routing,
// compliance, quota, and BYOK settings.
type GatewayProviderOptions struct {
	Only                   []string                       `json:"only,omitempty"`
	Order                  []string                       `json:"order,omitempty"`
	Sort                   string                         `json:"sort,omitempty"`
	User                   string                         `json:"user,omitempty"`
	Tags                   []string                       `json:"tags,omitempty"`
	Models                 []string                       `json:"models,omitempty"`
	BYOK                   map[string][]map[string]any    `json:"byok,omitempty"`
	ZeroDataRetention      *bool                          `json:"zeroDataRetention,omitempty"`
	DisallowPromptTraining *bool                          `json:"disallowPromptTraining,omitempty"`
	HIPAACompliant         *bool                          `json:"hipaaCompliant,omitempty"`
	QuotaEntityID          string                         `json:"quotaEntityId,omitempty"`
	ProviderTimeouts       *GatewayProviderTimeoutOptions `json:"providerTimeouts,omitempty"`
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
		out["models"] = o.Models
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
	if o.HIPAACompliant != nil {
		out["hipaaCompliant"] = *o.HIPAACompliant
	}
	if o.QuotaEntityID != "" {
		out["quotaEntityId"] = o.QuotaEntityID
	}
	if o.ProviderTimeouts != nil && len(o.ProviderTimeouts.BYOK) > 0 {
		out["providerTimeouts"] = map[string]interface{}{"byok": o.ProviderTimeouts.BYOK}
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
	ModelType     string                 `json:"modelType"`
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

	authResolver := func(ctx context.Context) (string, string, error) {
		return resolveGatewayAuthToken(ctx, cfg)
	}

	// Create headers with authentication
	headers := map[string]string{
		"ai-gateway-protocol-version": AIGatewayProtocolVersion,
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
		pendingMetadata:  &sync.Once{},
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
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("AI_GATEWAY_API_KEY")
	}
	if apiKey != "" {
		return apiKey, "api-key", nil
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
	return nil, fmt.Errorf("LGateway provider does not directly support speech synthesis models")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("LGateway provider does not directly support transcription models")
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

// GetAvailableModels returns available providers and models from the gateway
func (p *Provider) GetAvailableModels(ctx context.Context) (*MetadataResponse, error) {
	// Check if we have cached metadata and it's still fresh
	p.metadataMutex.RLock()
	if p.metadataCache != nil && time.Since(p.lastFetchTime) < p.cacheRefreshTime {
		cached := p.metadataCache
		p.metadataMutex.RUnlock()
		return cached, nil
	}
	p.metadataMutex.RUnlock()

	// Fetch fresh metadata
	resp, err := p.client.Get(ctx, "/config")
	if err != nil {
		return nil, p.handleError(err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, p.gatewayAPIError(resp)
	}
	var wire metadataResponseWire
	if err := json.Unmarshal(resp.Body, &wire); err != nil {
		return nil, p.handleError(err)
	}
	metadata := MetadataResponse{Models: make([]ModelMetadata, 0, len(wire.Models))}
	for _, model := range wire.Models {
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
			ModelType: model.ModelType,
		})
	}

	// Update cache
	p.metadataMutex.Lock()
	p.metadataCache = &metadata
	p.lastFetchTime = time.Now()
	p.metadataMutex.Unlock()

	return &metadata, nil
}

// GetCredits returns credit information for the authenticated user
func (p *Provider) GetCredits(ctx context.Context) (*CreditsInfo, error) {
	body, err := p.doOriginRequest(ctx, "/v1/credits")
	if err != nil {
		return nil, err
	}
	var wire struct {
		Balance   string `json:"balance"`
		TotalUsed string `json:"total_used"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, p.handleError(err)
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

func (p *Provider) handleError(err error) error {
	if err == nil {
		return nil
	}
	if gatewayerrors.IsTimeoutError(err) {
		return gatewayerrors.ConvertToGatewayTimeoutError(err, "gateway")
	}
	if gatewayerrors.IsGatewayError(err) {
		return err
	}
	if providererrors.IsProviderError(err) {
		return err
	}
	return providererrors.NewProviderError("gateway", 0, "", err.Error(), err)
}

func (p *Provider) doOriginRequest(ctx context.Context, path string) ([]byte, error) {
	client, err := p.originClient()
	if err != nil {
		return nil, p.handleError(err)
	}

	resp, err := client.Get(ctx, path)
	if err != nil {
		return nil, p.handleError(err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, p.gatewayAPIError(resp)
	}
	return resp.Body, nil
}

func (p *Provider) gatewayAPIError(resp *internalhttp.Response) error {
	if resp == nil {
		return gatewayerrors.NewGatewayResponseError("Gateway request failed", 0, nil, nil, nil, "")
	}
	authMethod := ""
	if p.authResolver != nil {
		_, authMethod, _ = p.authResolver(context.Background())
	}
	return gatewayerrors.CreateGatewayErrorFromResponse(resp.Body, resp.StatusCode, "Gateway request failed", nil, authMethod)
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

// NewTools creates a new Tools instance for accessing gateway-specific tools
func NewTools() *Tools {
	return &Tools{}
}

// Default gateway provider instance
var defaultProvider *Provider

// Gateway returns the default gateway provider
// It reads configuration from environment variables
func Gateway() (*Provider, error) {
	if defaultProvider == nil {
		var err error
		defaultProvider, err = New(Config{})
		if err != nil {
			return nil, err
		}
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
