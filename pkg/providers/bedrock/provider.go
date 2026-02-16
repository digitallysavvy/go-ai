package bedrock

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for AWS Bedrock
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the AWS Bedrock provider
type Config struct {
	// AWSAccessKeyID is the AWS access key ID
	AWSAccessKeyID string

	// AWSSecretAccessKey is the AWS secret access key
	AWSSecretAccessKey string

	// Region is the AWS region (e.g., "us-east-1")
	Region string

	// SessionToken is an optional AWS session token for temporary credentials
	SessionToken string
}

// New creates a new AWS Bedrock provider with the given configuration
func New(cfg Config) *Provider {
	// AWS Bedrock endpoint
	baseURL := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", cfg.Region)

	// Note: AWS Signature V4 signing would be required for real implementation
	// For now, we'll create a basic client structure
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "aws-bedrock"
}

// LanguageModel returns a language model by ID
// Model IDs for Bedrock look like: "anthropic.claude-3-sonnet-20240229-v1:0"
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewLanguageModel(p, modelID), nil
}

// LanguageModelWithOptions returns a language model with custom options
func (p *Provider) LanguageModelWithOptions(modelID string, options *ModelOptions) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewLanguageModel(p, modelID, options), nil
}

// EmbeddingModel returns an embedding model by ID with default options
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// EmbeddingModelWithOptions returns an embedding model by ID with custom options
func (p *Provider) EmbeddingModelWithOptions(modelID string, options *EmbeddingOptions) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewEmbeddingModel(p, modelID, options), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Bedrock supports Stable Diffusion models
	if modelID == "" {
		modelID = "stability.stable-diffusion-xl-v1"
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	// Bedrock doesn't provide TTS models directly
	return nil, fmt.Errorf("AWS Bedrock does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Bedrock doesn't provide STT models directly
	return nil, fmt.Errorf("AWS Bedrock does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	// Bedrock doesn't provide reranking models directly
	return nil, fmt.Errorf("AWS Bedrock does not support reranking")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// Region returns the AWS region
func (p *Provider) Region() string {
	return p.config.Region
}
