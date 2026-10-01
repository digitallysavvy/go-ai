package openresponses

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// defaultCustomToolID is Config.CustomToolID's default, mirroring the TS
// SDK's `'open-responses.custom'` default in open-responses-provider.ts.
const defaultCustomToolID = "open-responses.custom"

// Provider implements the provider.Provider interface for Open Responses API
// This provider enables compatibility with local LLMs (LMStudio, Ollama) and
// other services that implement the OpenAI Responses API format.
type Provider struct {
	config            Config
	client            *http.Client
	extensionRegistry *ExtensionRegistry
}

// Config contains configuration for the Open Responses provider
type Config struct {
	// BaseURL is the base URL for the Open Responses API endpoint
	// Examples:
	//   - LMStudio: "http://localhost:1234/v1"
	//   - Ollama: "http://localhost:11434/v1"
	//   - LocalAI: "http://localhost:8080/v1"
	BaseURL string

	// APIKey is the optional API key for authentication
	// Many local model servers don't require authentication
	APIKey string

	// Headers are custom HTTP headers to include in requests
	Headers map[string]string

	// Name is the provider name for identification (default: "open-responses")
	Name string

	// StrictResponseInput controls how assistant history is serialized back to
	// the Responses API. When true, assistant text with no known item ID is
	// sent as a plain string "message" input item, while assistant text with a
	// known item ID is replayed as a complete "output_text" output item
	// (status "completed", with annotations/logprobs arrays). Mirrors the
	// TypeScript SDK's `strictResponseInput` provider setting.
	//
	// @default false
	StrictResponseInput bool

	// Extensions registers codecs for Open Responses extension tools,
	// items, and streaming events (row 9a68261, OR-EXT). Most Open
	// Responses servers need none of this; see Extension for details.
	Extensions []Extension

	// FailedResponseHandler, when set, converts a non-2xx HTTP response into
	// an error, overriding the default generic HTTPStatusError wrapping.
	// Mirrors the TS SDK's `failedResponseHandler` provider setting (e.g.
	// QuiverAI's endpoint-specific {status,code,message,request_id} error
	// schema). Returning nil falls back to the default handling.
	FailedResponseHandler func(*http.HTTPStatusError) error

	// GetResponseErrorMetadata extracts HTTP status and retryability
	// metadata from an endpoint-specific response error embedded in a 200
	// response body (`response.error`) or a streamed response.failed/error
	// event, mirroring the TS SDK's `getResponseErrorMetadata` provider
	// setting. Either return value may be nil to fall back to the default
	// classification (statusCode 400 for a non-streaming embedded error;
	// message/status-code inference for a streamed error).
	GetResponseErrorMetadata func(*ResponseError) (statusCode *int, retryable *bool)

	// CustomToolID identifies caller-executed Open Responses custom tools by
	// provider-tool ID (types.Tool.ProviderID). A "provider" tool whose ID
	// matches is encoded as {"type":"custom",...} instead of going through
	// the Extensions registry, mirroring the TS SDK's `customToolId`
	// provider setting. Defaults to "open-responses.custom" when unset (New
	// applies the default), matching the TS SDK's `customToolId ??
	// 'open-responses.custom'` -- unlike the other Config fields, this is
	// never actually "off": every Open Responses provider recognizes some
	// custom-tool id, even one that never configured this field.
	CustomToolID string

	// StructuredOutputs controls whether JSON response formats are sent to
	// the endpoint. Nil or true (default) sends them; explicit false treats
	// a JSON responseFormat as unsupported (warning + omitted), mirroring
	// the TS SDK's `structuredOutputs` provider setting.
	StructuredOutputs *bool

	// UserAgentSuffix overrides the User-Agent suffix appended to requests.
	// Defaults to "ai-sdk/<Name>/<version>", mirroring the TS SDK's
	// `userAgentSuffix` provider setting (default
	// `ai-sdk/open-responses/${VERSION}`). A wrapper provider built on this
	// package (e.g. QuiverAI) sets this to identify itself instead of the
	// generic "open-responses" name.
	UserAgentSuffix string
}

// New creates a new Open Responses provider with the given configuration
func New(cfg Config) *Provider {
	// Validate base URL
	if cfg.BaseURL == "" {
		panic("openresponses: baseURL is required")
	}

	// Set default provider name
	if cfg.Name == "" {
		cfg.Name = "open-responses"
	}

	// Default CustomToolID, mirroring TS's `options.customToolId ??
	// 'open-responses.custom'` -- always set, not merely "enabled when
	// configured".
	if cfg.CustomToolID == "" {
		cfg.CustomToolID = defaultCustomToolID
	}

	// Build headers
	headers := make(map[string]string)
	headers["Content-Type"] = "application/json"

	// Add authorization header if API key is provided
	if cfg.APIKey != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", cfg.APIKey)
	}

	// Add custom headers
	for k, v := range cfg.Headers {
		headers[k] = v
	}
	userAgentSuffix := cfg.UserAgentSuffix
	if userAgentSuffix == "" {
		userAgentSuffix = version.ProviderUserAgent(cfg.Name)
	}
	headers = version.WithUserAgentSuffix(headers, userAgentSuffix)

	// Create HTTP client
	client := http.NewClient(http.Config{
		BaseURL: cfg.BaseURL,
		Headers: headers,
	})

	registry, err := NewExtensionRegistry(cfg.Extensions)
	if err != nil {
		panic("openresponses: " + err.Error())
	}

	return &Provider{
		config:            cfg,
		client:            client,
		extensionRegistry: registry,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return p.config.Name
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
	return nil, fmt.Errorf("%w: %s embeddingModel %q", providererrors.ErrModelNotFound, p.Name(), modelID)
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("%w: %s imageModel %q", providererrors.ErrModelNotFound, p.Name(), modelID)
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("open responses provider does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support transcription") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Open Responses provider does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// Tools returns a factory for this provider's caller-executed Open
// Responses tools (currently just CustomTool), scoped to
// Config.CustomToolID (which New defaults to "open-responses.custom" when
// unset, so this is always available), mirroring TS's `provider.tools =
// createOpenResponsesTools({customToolId})`.
func (p *Provider) Tools() *Tools {
	return NewTools(p.config.CustomToolID)
}
