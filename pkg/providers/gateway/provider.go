package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway/tools"
)

const (
	// DefaultBaseURL is the default AI Gateway API base URL
	DefaultBaseURL = "https://ai-gateway.vercel.sh/v3/ai"

	// AIGatewayProtocolVersion is the protocol version for the AI Gateway
	AIGatewayProtocolVersion = "0.0.1"

	// DefaultMetadataCacheRefresh is the default time to refresh metadata cache (5 minutes)
	DefaultMetadataCacheRefresh = 5 * time.Minute
)

// Provider implements the provider.Provider interface for AI Gateway
type Provider struct {
	config           Config
	client           *internalhttp.Client
	metadataCache    *MetadataResponse
	metadataMutex    sync.RWMutex
	lastFetchTime    time.Time
	pendingMetadata  *sync.Once
	cacheRefreshTime time.Duration
}

// Config contains configuration for the AI Gateway provider
type Config struct {
	// APIKey is the AI Gateway API key
	// Can also be set via AI_GATEWAY_API_KEY environment variable
	APIKey string

	// BaseURL is the base URL for the AI Gateway API
	// Default: https://ai-gateway.vercel.sh/v3/ai
	BaseURL string

	// Headers are custom headers to include in requests
	Headers map[string]string

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
	Providers []ProviderMetadata `json:"providers"`
	Credits   *CreditsInfo       `json:"credits,omitempty"`
}

// ProviderMetadata contains information about a provider
type ProviderMetadata struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Models []ModelMetadata `json:"models"`
}

// ModelMetadata contains information about a model
type ModelMetadata struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// CreditsInfo contains credit information for the authenticated user
type CreditsInfo struct {
	Available int `json:"available"`
	Used      int `json:"used"`
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

	// Get API key from config or environment
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("AI_GATEWAY_API_KEY")
	}

	if apiKey == "" {
		return nil, fmt.Errorf("AI Gateway API key is required (set via Config.APIKey or AI_GATEWAY_API_KEY environment variable)")
	}

	// Create headers with authentication
	headers := map[string]string{
		"Authorization":               fmt.Sprintf("Bearer %s", apiKey),
		"ai-gateway-protocol-version": AIGatewayProtocolVersion,
		"ai-gateway-auth-method":      "api-key",
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

	// Create HTTP client
	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		Headers:    headers,
		HTTPClient: cfg.HTTPClient,
	})

	// Set cache refresh time
	cacheRefreshTime := DefaultMetadataCacheRefresh
	if cfg.MetadataCacheRefreshMillis > 0 {
		cacheRefreshTime = time.Duration(cfg.MetadataCacheRefreshMillis) * time.Millisecond
	}

	return &Provider{
		config:           cfg,
		client:           client,
		cacheRefreshTime: cacheRefreshTime,
		pendingMetadata:  &sync.Once{},
	}, nil
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
	return nil, fmt.Errorf("Gateway provider does not directly support speech synthesis models")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Gateway provider does not directly support transcription models")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Gateway provider does not directly support reranking models")
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
	var metadata MetadataResponse
	err := p.client.GetJSON(ctx, "/metadata", &metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch available models: %w", err)
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
	var credits CreditsInfo
	err := p.client.GetJSON(ctx, "/credits", &credits)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch credits: %w", err)
	}
	return &credits, nil
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
func GetO11yHeaders() O11yHeaders {
	return O11yHeaders{
		DeploymentID: os.Getenv("VERCEL_DEPLOYMENT_ID"),
		Environment:  os.Getenv("VERCEL_ENV"),
		Region:       os.Getenv("VERCEL_REGION"),
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
