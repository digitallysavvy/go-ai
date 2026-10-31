package mantle

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

	// CredentialProvider returns dynamic AWS credentials for SigV4 signing.
	// When set, it takes precedence over AccessKeyID, SecretAccessKey,
	// SessionToken, and AWS credential environment variables.
	CredentialProvider bedrock.CredentialProvider
}

// mantleSigningService is the SigV4 service name Mantle requests are signed
// with (TS passes 'bedrock-mantle' to createSigV4FetchFunction).
const mantleSigningService = "bedrock-mantle"

// mantleOpenAIOnlyModelPattern matches model IDs Mantle serves under its
// separate OpenAI-compatible route (`/openai/v1`) rather than the default
// `/v1`. Ports TS mantle/bedrock-mantle-provider.ts's getBaseURL regex:
// `^(?:openai\.gpt-(?!oss-)|google\.gemma-4|xai\.)`. gpt-oss-* models are
// excluded (they use the default /v1 route).
var mantleOpenAIOnlyModelPattern = regexp.MustCompile(`^(?:openai\.gpt-|google\.gemma-4|xai\.)`)

func isMantleOpenAIOnlyModel(modelID string) bool {
	if !mantleOpenAIOnlyModelPattern.MatchString(modelID) {
		return false
	}
	// The regex above can't express a negative lookahead in Go's RE2 engine,
	// so gpt-oss-* is excluded explicitly here instead of via `(?!oss-)`.
	if strings.HasPrefix(modelID, "openai.gpt-oss-") {
		return false
	}
	return true
}

// BedrockMantleProvider exposes OpenAI-compatible Chat Completions and
// Responses models through Amazon Bedrock Mantle.
type BedrockMantleProvider struct {
	settings   ProviderSettings
	region     string
	apiKey     string
	httpClient *http.Client
	headers    map[string]string
}

// CreateBedrockMantle creates a Bedrock Mantle provider.
func CreateBedrockMantle(settings ProviderSettings) *BedrockMantleProvider {
	region := firstNonEmpty(settings.Region, os.Getenv("AWS_REGION"))
	settings.Region = region

	// Credentials (bearer token or SigV4) are resolved per request by the
	// transport, so AWS_BEARER_TOKEN_BEDROCK / AWS_* set or rotated after the
	// provider was created are honored (TS loadApiKey semantics).
	apiKey := settings.APIKey
	headers := copyHeaders(settings.Headers)
	httpClient := withAuthTransport(settings.HTTPClient, settings, region)

	return &BedrockMantleProvider{
		settings:   settings,
		region:     region,
		apiKey:     apiKey,
		httpClient: httpClient,
		headers:    headers,
	}
}

// New creates a Bedrock Mantle provider.
func New(settings ProviderSettings) *BedrockMantleProvider {
	return CreateBedrockMantle(settings)
}

// Name returns the provider name.
func (p *BedrockMantleProvider) Name() string { return "bedrock-mantle" }

// baseURLForModel resolves the Mantle base URL for modelID. An explicit
// settings.BaseURL always wins; otherwise the URL depends on whether modelID
// is one Mantle serves through its separate OpenAI-compatible route.
func (p *BedrockMantleProvider) baseURLForModel(modelID string) (string, error) {
	if p.settings.BaseURL != "" {
		return strings.TrimSuffix(p.settings.BaseURL, "/"), nil
	}
	// Region is interpolated directly into the request host
	// (https://bedrock-mantle.{region}.api.aws/...), so only a single DNS
	// label is accepted; ports TS bedrock-mantle-provider.ts (TS #21842).
	// TS loadSetting reads AWS_REGION lazily on each request URL build, so a
	// region exported after the provider (or the default instance) was created
	// is still honored.
	region := firstNonEmpty(p.settings.Region, os.Getenv("AWS_REGION"))
	if region == "" {
		return "", fmt.Errorf("AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable")
	}
	if !providerutils.IsValidHostnamePart(region) {
		return "", &providererrors.InvalidArgumentError{
			Field:   "region",
			Message: "Invalid AWS region. Expected a single DNS label (letters, digits, and hyphens). Use `baseURL` for custom endpoints.",
		}
	}
	path := "v1"
	if isMantleOpenAIOnlyModel(modelID) {
		path = "openai/v1"
	}
	return fmt.Sprintf("https://bedrock-mantle.%s.api.aws/%s", region, path), nil
}

func (p *BedrockMantleProvider) openaiProviderForModel(modelID string) (*openai.Provider, error) {
	baseURL, err := p.baseURLForModel(modelID)
	if err != nil {
		return nil, err
	}
	return openai.New(openai.Config{
		APIKey:           p.apiKey,
		Name:             "bedrock-mantle",
		BaseURL:          baseURL,
		Headers:          p.headers,
		HTTPClient:       p.httpClient,
		ChatProviderName: "bedrock-mantle.chat",
		UserAgentName:    "amazon-bedrock",
	}), nil
}

// LanguageModel returns a chat-completions-compatible model.
func (p *BedrockMantleProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.Chat(modelID)
}

// Chat returns a chat-completions-compatible model.
func (p *BedrockMantleProvider) Chat(modelID string) (provider.LanguageModel, error) {
	oai, err := p.openaiProviderForModel(modelID)
	if err != nil {
		return nil, err
	}
	return oai.ChatModel(modelID)
}

// ChatModel is an alias for Chat.
func (p *BedrockMantleProvider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.Chat(modelID)
}

// Responses returns a Responses API-compatible model.
func (p *BedrockMantleProvider) Responses(modelID string) (provider.LanguageModel, error) {
	baseURL, err := p.baseURLForModel(modelID)
	if err != nil {
		return nil, err
	}
	supportsWebSearchSourcesInclude := false
	oai := openai.New(openai.Config{
		APIKey:                          p.apiKey,
		Name:                            "bedrock-mantle",
		BaseURL:                         baseURL,
		Headers:                         p.headers,
		HTTPClient:                      p.httpClient,
		ChatProviderName:                "bedrock-mantle.chat",
		ResponsesProviderName:           "bedrock-mantle.responses",
		SupportsWebSearchSourcesInclude: &supportsWebSearchSourcesInclude,
		UserAgentName:                   "amazon-bedrock",
	})
	return oai.ResponsesModel(modelID)
}

// ResponsesModel is an alias for Responses.
func (p *BedrockMantleProvider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	return p.Responses(modelID)
}

// EmbeddingModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, noSuchModelError(modelID, "embeddingModel")
}

// ImageModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, noSuchModelError(modelID, "imageModel")
}

// SpeechModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel is not supported by Bedrock Mantle.
func (p *BedrockMantleProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
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

func noSuchModelError(modelID, modelType string) error {
	return providererrors.NewNoSuchModelError(modelID, modelType)
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

func withAuthTransport(client *http.Client, settings ProviderSettings, region string) *http.Client {
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
	client.Transport = &sigV4Transport{
		base:               base,
		apiKey:             settings.APIKey,
		accessKeyID:        settings.AccessKeyID,
		secretAccessKey:    settings.SecretAccessKey,
		sessionToken:       settings.SessionToken,
		credentialProvider: settings.CredentialProvider,
		region:             region,
	}
	return client
}
