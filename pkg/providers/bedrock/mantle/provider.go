package mantle

import (
	"fmt"
	"net/http"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// ProviderSettings contains configuration for the Amazon Bedrock Mantle provider.
type ProviderSettings struct {
	Region          string
	APIKey          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	BaseURL         string
	Headers         map[string]string
	HTTPClient      *http.Client
}

// BedrockMantleProvider exposes OpenAI-compatible Chat Completions and
// Responses models through Amazon Bedrock Mantle.
type BedrockMantleProvider struct {
	settings ProviderSettings
	openai   *openai.Provider
}

// CreateBedrockMantle creates a Bedrock Mantle provider.
func CreateBedrockMantle(settings ProviderSettings) *BedrockMantleProvider {
	region := firstNonEmpty(settings.Region, os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	baseURL := settings.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://bedrock-mantle.%s.api.aws/v1", region)
	}

	apiKey := firstNonEmpty(settings.APIKey, os.Getenv("AWS_BEARER_TOKEN_BEDROCK"))
	httpClient := settings.HTTPClient
	headers := copyHeaders(settings.Headers)
	if apiKey == "" {
		httpClient = withSigV4Transport(httpClient, bedrock.NewAWSSigner(
			firstNonEmpty(settings.AccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID")),
			firstNonEmpty(settings.SecretAccessKey, os.Getenv("AWS_SECRET_ACCESS_KEY")),
			firstNonEmpty(settings.SessionToken, os.Getenv("AWS_SESSION_TOKEN")),
			region,
		))
	}

	return &BedrockMantleProvider{
		settings: settings,
		openai: openai.New(openai.Config{
			APIKey:     apiKey,
			Name:       "bedrock-mantle.chat",
			BaseURL:    baseURL,
			Headers:    headers,
			HTTPClient: httpClient,
		}),
	}
}

// New creates a Bedrock Mantle provider.
func New(settings ProviderSettings) *BedrockMantleProvider {
	return CreateBedrockMantle(settings)
}

// Name returns the provider name.
func (p *BedrockMantleProvider) Name() string { return "bedrock-mantle" }

// LanguageModel returns a chat-completions-compatible model.
func (p *BedrockMantleProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.Chat(modelID)
}

// Chat returns a chat-completions-compatible model.
func (p *BedrockMantleProvider) Chat(modelID string) (provider.LanguageModel, error) {
	return p.openai.LanguageModel(modelID)
}

// ChatModel is an alias for Chat.
func (p *BedrockMantleProvider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.Chat(modelID)
}

// Responses returns a Responses API-compatible model.
func (p *BedrockMantleProvider) Responses(modelID string) (provider.LanguageModel, error) {
	return p.openai.ResponsesModel(modelID)
}

// ResponsesModel is an alias for Responses.
func (p *BedrockMantleProvider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	return p.Responses(modelID)
}

// EmbeddingModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("bedrock-mantle provider does not support embedding models")
}

// ImageModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("bedrock-mantle provider does not support image models")
}

// SpeechModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("bedrock-mantle provider does not support speech models")
}

// TranscriptionModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("bedrock-mantle provider does not support transcription models")
}

// RerankingModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("bedrock-mantle provider does not support reranking models")
}

// BedrockMantle is the default provider instance.
var BedrockMantle = CreateBedrockMantle(ProviderSettings{})

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func withSigV4Transport(client *http.Client, signer *bedrock.AWSSigner) *http.Client {
	if client == nil {
		client = &http.Client{}
	} else {
		clone := *client
		client = &clone
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &sigV4Transport{base: base, signer: signer}
	return client
}
