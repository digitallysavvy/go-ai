package openai

import (
	"fmt"
	stdhttp "net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default OpenAI API base URL
	DefaultBaseURL = "https://api.openai.com/v1"
)

// Provider implements the provider.Provider interface for OpenAI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the OpenAI provider
type Config struct {
	// APIKey is the OpenAI API key
	APIKey string

	// Name overrides the provider name returned by Provider.Name().
	// Defaults to "openai".
	Name string

	// BaseURL is the base URL for the OpenAI API (default: https://api.openai.com/v1)
	BaseURL string

	// Organization is the optional organization ID
	Organization string

	// Project is the optional project ID
	Project string

	// HTTPClient overrides the HTTP client used for all requests.
	HTTPClient *stdhttp.Client

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`

	// ChatProviderName overrides the provider identifier returned by ChatModel.
	// Defaults to "openai.chat".
	ChatProviderName string

	// CompletionProviderName overrides the provider identifier returned by
	// CompletionModel. Defaults to "openai.completion".
	CompletionProviderName string

	// CompletionProviderOptionsName selects the providerOptions/providerMetadata
	// namespace used by completion models. Defaults to "azure" when the
	// completion provider name contains "azure", otherwise "openai".
	CompletionProviderOptionsName string

	// CompletionQuery contains query parameters added to Completions API requests.
	CompletionQuery map[string]string

	// ResponsesProviderName overrides the provider identifier returned by
	// ResponsesModel. Defaults to "openai.responses".
	ResponsesProviderName string

	// ResponsesProviderOptionsName selects the providerOptions/providerMetadata
	// namespace used by Responses models. Defaults to "azure" when the
	// Responses provider name contains "azure", otherwise "openai".
	ResponsesProviderOptionsName string

	// ResponsesQuery contains query parameters added to Responses API requests.
	ResponsesQuery map[string]string

	// FileIDPrefixes identifies deprecated string file-data values that should
	// be sent to the Responses API as file_id references instead of base64 data.
	// Nil defaults to []string{"file-"} to match the TypeScript OpenAI provider;
	// set an empty non-nil slice to disable this compatibility path.
	FileIDPrefixes []string
}

// New creates a new OpenAI provider with the given configuration
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// Create HTTP client with default headers
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", cfg.APIKey)
	}

	if cfg.Organization != "" {
		headers["OpenAI-Organization"] = cfg.Organization
	}

	if cfg.Project != "" {
		headers["OpenAI-Project"] = cfg.Project
	}

	client := http.NewClient(http.Config{
		BaseURL:    baseURL,
		Headers:    http.MergeHeaders(headers, cfg.Headers),
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateOpenAI creates a new OpenAI provider.
//
// It mirrors the TypeScript SDK createOpenAI export while New remains the
// idiomatic Go constructor.
func CreateOpenAI(cfg Config) *Provider {
	return New(cfg)
}

func (p *Provider) responsesFileIDPrefixes() []string {
	if p.config.FileIDPrefixes != nil {
		return p.config.FileIDPrefixes
	}
	return []string{"file-"}
}

func (p *Provider) responsesProviderName() string {
	if p.config.ResponsesProviderName != "" {
		return p.config.ResponsesProviderName
	}
	if name := p.Name(); name != "" && name != "openai" {
		return name + ".responses"
	}
	return "openai.responses"
}

func (p *Provider) responsesQuery() map[string]string {
	if len(p.config.ResponsesQuery) == 0 {
		return nil
	}
	query := make(map[string]string, len(p.config.ResponsesQuery))
	for k, v := range p.config.ResponsesQuery {
		query[k] = v
	}
	return query
}

func (p *Provider) responsesProviderOptionsName() string {
	if p.config.ResponsesProviderOptionsName != "" {
		return p.config.ResponsesProviderOptionsName
	}
	if strings.Contains(p.responsesProviderName(), "azure") {
		return "azure"
	}
	return "openai"
}

func (p *Provider) completionProviderName() string {
	if p.config.CompletionProviderName != "" {
		return p.config.CompletionProviderName
	}
	if name := p.Name(); name != "" && name != "openai" {
		return name + ".completion"
	}
	return "openai.completion"
}

func (p *Provider) completionQuery() map[string]string {
	if len(p.config.CompletionQuery) == 0 {
		return nil
	}
	query := make(map[string]string, len(p.config.CompletionQuery))
	for k, v := range p.config.CompletionQuery {
		query[k] = v
	}
	return query
}

func (p *Provider) completionProviderOptionsName() string {
	if p.config.CompletionProviderOptionsName != "" {
		return p.config.CompletionProviderOptionsName
	}
	if strings.Contains(p.completionProviderName(), "azure") {
		return "azure"
	}
	return "openai"
}

// Name returns the provider name
func (p *Provider) Name() string {
	if p.config.Name != "" {
		return p.config.Name
	}
	return "openai"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.ResponsesModel(modelID)
}

// ChatModel returns a Chat Completions API language model by ID. It mirrors the
// TypeScript provider.chat factory while LanguageModel follows the TypeScript
// default provider function and returns a Responses API model.
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewLanguageModel(p, modelID), nil
}

// CompletionModel returns a language model that uses the OpenAI Completions API
// (/v1/completions). This mirrors the TypeScript provider.completion factory for
// instruct-style completion models.
func (p *Provider) CompletionModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewCompletionModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "dall-e-3"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	if modelID == "" {
		modelID = "tts-1"
	}

	return NewSpeechModel(p, modelID), nil
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if modelID == "" {
		modelID = "whisper-1"
	}

	return NewTranscriptionModel(p, modelID), nil
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// OpenAI doesn't provide reranking models
	return nil, fmt.Errorf("LOpenAI does not support reranking")
}

// ResponsesModel returns a language model that uses the OpenAI Responses API
// (/v1/responses) instead of Chat Completions. Use this for models that benefit
// from stateful conversations, tool search, compaction, or Responses-only features.
func (p *Provider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewResponsesLanguageModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

func (p *Provider) Files() provider.FilesAPI {
	return &FilesAPI{provider: p}
}

func (p *Provider) Skills() provider.SkillsAPI {
	return &SkillsAPI{provider: p}
}
