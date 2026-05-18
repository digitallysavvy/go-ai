package anthropic

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	anthropicprovider "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

const (
	DefaultProviderName      = "googleVertex.anthropic.messages"
	DefaultVertexAPIVersion  = "vertex-2023-10-16"
	defaultCloudPlatformAuth = "https://www.googleapis.com/auth/cloud-platform"
)

// GoogleVertexAnthropicProvider routes Claude requests through Vertex AI's
// Anthropic publisher endpoint.
type GoogleVertexAnthropicProvider struct {
	options Options
}

// NewGoogleVertexAnthropicProvider creates a Google Vertex Anthropic provider.
func NewGoogleVertexAnthropicProvider(opts Options) *GoogleVertexAnthropicProvider {
	return &GoogleVertexAnthropicProvider{options: opts}
}

// New is the idiomatic short constructor.
func New(opts Options) *GoogleVertexAnthropicProvider {
	return NewGoogleVertexAnthropicProvider(opts)
}

// CreateGoogleVertexAnthropic mirrors the TypeScript SDK export.
func CreateGoogleVertexAnthropic(opts Options) *GoogleVertexAnthropicProvider {
	return NewGoogleVertexAnthropicProvider(opts)
}

// CreateVertexAnthropic mirrors the TypeScript SDK's deprecated export.
//
// Deprecated: use CreateGoogleVertexAnthropic.
func CreateVertexAnthropic(opts Options) *GoogleVertexAnthropicProvider {
	return CreateGoogleVertexAnthropic(opts)
}

// GoogleVertexAnthropic is the default provider instance.
var GoogleVertexAnthropic = NewGoogleVertexAnthropicProvider(Options{})

// VertexAnthropic is a deprecated alias for GoogleVertexAnthropic.
//
// Deprecated: use GoogleVertexAnthropic.
var VertexAnthropic = GoogleVertexAnthropic

// Name returns the provider name.
func (p *GoogleVertexAnthropicProvider) Name() string {
	return "google-vertex-anthropic"
}

// LanguageModel creates a Claude language model for Vertex Anthropic.
func (p *GoogleVertexAnthropicProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModelWithOptions(modelID, nil)
}

// LanguageModelWithOptions creates a Claude language model with Anthropic
// provider-specific settings. The settings surface is inherited from the direct
// Anthropic provider.
func (p *GoogleVertexAnthropicProvider) LanguageModelWithOptions(modelID string, settings *anthropicprovider.ModelOptions) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	baseURL, err := p.baseURL()
	if err != nil {
		return nil, err
	}
	httpClient := p.httpClient()

	nativeStructuredOutput := false
	supportsImageInput := true
	supportsStrictTools := false
	anthropicProvider := anthropicprovider.New(anthropicprovider.Config{
		Name:                           DefaultProviderName,
		BaseURL:                        baseURL,
		OmitAPIVersionHeader:           true,
		HTTPClient:                     httpClient,
		SupportsNativeStructuredOutput: &nativeStructuredOutput,
		SupportsImageInput:             &supportsImageInput,
		SupportsStrictTools:            &supportsStrictTools,
		MessagesPath: func(id string, stream bool) string {
			action := "rawPredict"
			if stream {
				action = "streamRawPredict"
			}
			return fmt.Sprintf("/%s:%s", id, action)
		},
		TransformRequestBody: func(body map[string]interface{}, stream bool) map[string]interface{} {
			out := make(map[string]interface{}, len(body)+1)
			for k, v := range body {
				if k == "model" {
					continue
				}
				out[k] = v
			}
			out["anthropic_version"] = DefaultVertexAPIVersion
			return out
		},
	})

	return anthropicprovider.NewLanguageModel(anthropicProvider, modelID, settings), nil
}

// ChatModel is an alias for LanguageModel.
func (p *GoogleVertexAnthropicProvider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// Chat is an alias for LanguageModel.
func (p *GoogleVertexAnthropicProvider) Chat(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// MessagesModel is an alias for LanguageModel.
func (p *GoogleVertexAnthropicProvider) MessagesModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// Messages is an alias for LanguageModel.
func (p *GoogleVertexAnthropicProvider) Messages(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

func (p *GoogleVertexAnthropicProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("google vertex anthropic does not support embedding models")
}

// TextEmbeddingModel is a deprecated alias for EmbeddingModel.
//
// Deprecated: use EmbeddingModel.
func (p *GoogleVertexAnthropicProvider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

func (p *GoogleVertexAnthropicProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("google vertex anthropic does not support image models")
}

func (p *GoogleVertexAnthropicProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("google vertex anthropic does not support speech synthesis")
}

func (p *GoogleVertexAnthropicProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("google vertex anthropic does not support transcription")
}

func (p *GoogleVertexAnthropicProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("google vertex anthropic does not support reranking")
}

func (p *GoogleVertexAnthropicProvider) baseURL() (string, error) {
	if p.options.BaseURL != "" {
		return strings.TrimRight(p.options.BaseURL, "/"), nil
	}

	project := p.options.Project
	if project == "" {
		project = os.Getenv("GOOGLE_VERTEX_PROJECT")
	}
	if project == "" {
		return "", fmt.Errorf("project is required for Google Vertex Anthropic")
	}

	location := p.options.Location
	if location == "" {
		location = os.Getenv("GOOGLE_VERTEX_LOCATION")
	}
	if location == "" {
		return "", fmt.Errorf("location is required for Google Vertex Anthropic")
	}

	host := "aiplatform.googleapis.com"
	if location != "global" {
		host = location + "-" + host
	}
	return fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/publishers/anthropic/models", host, project, location), nil
}

func (p *GoogleVertexAnthropicProvider) httpClient() *http.Client {
	baseClient := http.DefaultClient
	if p.options.HTTPClient != nil {
		baseClient = p.options.HTTPClient
	}
	clone := *baseClient
	baseTransport := clone.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	if transport, ok := baseTransport.(*http.Transport); ok {
		baseTransport = transport.Clone()
	}

	tokenFunc := p.options.AuthToken
	if tokenFunc == nil {
		tokenFunc = p.lazyAuthToken()
	}

	clone.Transport = &authTransport{
		base:        baseTransport,
		tokenFunc:   tokenFunc,
		headers:     p.options.Headers,
		headersFunc: p.options.HeadersResolver,
	}
	return &clone
}
