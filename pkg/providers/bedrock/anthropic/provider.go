package anthropic

import (
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultRegion is the default AWS region
	DefaultRegion = "us-east-1"

	// BaseURLFormat for Bedrock runtime endpoints
	BaseURLFormat = "https://bedrock-runtime.%s.amazonaws.com"

	// AnthropicVersion is the Bedrock Anthropic API version
	AnthropicVersion = "bedrock-2023-05-31"
)

// AWSCredentials contains AWS authentication information
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string // Optional for temporary credentials
}

// Config for Bedrock Anthropic provider
type Config struct {
	// Region is the AWS region (e.g., "us-east-1")
	Region string

	// Credentials for AWS SigV4 authentication
	// If nil, will attempt bearer token authentication
	Credentials *AWSCredentials

	// BearerToken for alternative authentication
	// Set via AWS_BEARER_TOKEN_BEDROCK environment variable
	// If both Credentials and BearerToken are provided, BearerToken takes precedence
	BearerToken string

	// BaseURL overrides the default Bedrock endpoint
	// Default: https://bedrock-runtime.{region}.amazonaws.com
	BaseURL string

	// HTTPClient allows custom HTTP client configuration
	HTTPClient *http.Client
}

// BedrockAnthropicProvider implements native Anthropic Messages API via AWS Bedrock
type BedrockAnthropicProvider struct {
	region      string
	credentials *AWSCredentials
	bearerToken string
	baseURL     string
	httpClient  *http.Client

	// Tool version mappings for Bedrock compatibility
	toolVersionMap map[string]string

	// Tool name mappings for Bedrock requirements
	toolNameMap map[string]string

	// Beta header mappings for computer use tools
	toolBetaMap map[string]string
}

// New creates a new Bedrock Anthropic provider
func New(config Config) *BedrockAnthropicProvider {
	region := config.Region
	if region == "" {
		region = DefaultRegion
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf(BaseURLFormat, region)
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &BedrockAnthropicProvider{
		region:      region,
		credentials: config.Credentials,
		bearerToken: config.BearerToken,
		baseURL:     baseURL,
		httpClient:  httpClient,

		// Tool version upgrades for Bedrock compatibility
		toolVersionMap: map[string]string{
			"bash_20241022":        "bash_20250124",
			"text_editor_20241022": "text_editor_20250728",
			"computer_20241022":    "computer_20250124",
		},

		// Tool name mappings for specific versions
		toolNameMap: map[string]string{
			"text_editor_20250728": "str_replace_based_edit_tool",
		},

		// Beta headers for computer use tools
		toolBetaMap: map[string]string{
			"bash_20250124":        "computer-use-2025-01-24",
			"bash_20241022":        "computer-use-2024-10-22",
			"text_editor_20250124": "computer-use-2025-01-24",
			"text_editor_20241022": "computer-use-2024-10-22",
			"text_editor_20250429": "computer-use-2025-01-24",
			"text_editor_20250728": "computer-use-2025-01-24",
			"computer_20250124":    "computer-use-2025-01-24",
			"computer_20241022":    "computer-use-2024-10-22",
		},
	}
}

// Name returns the provider name
func (p *BedrockAnthropicProvider) Name() string {
	return "bedrock-anthropic"
}

// LanguageModel returns a language model instance
func (p *BedrockAnthropicProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return &BedrockAnthropicLanguageModel{
		provider: p,
		modelID:  modelID,
	}, nil
}

// EmbeddingModel returns an embedding model (not supported)
func (p *BedrockAnthropicProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support embedding models")
}

// ImageModel returns an image model (not supported)
func (p *BedrockAnthropicProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support image models")
}

// SpeechModel returns a speech model (not supported)
func (p *BedrockAnthropicProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support speech models")
}

// TranscriptionModel returns a transcription model (not supported)
func (p *BedrockAnthropicProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support transcription models")
}

// RerankingModel returns a reranking model (not supported)
func (p *BedrockAnthropicProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support reranking models")
}
