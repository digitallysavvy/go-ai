// Package revai provides a Rev.ai transcription provider for the Go AI SDK.
// Rev.ai offers an asynchronous job-based speech-to-text flow: submit a job
// (POST /speechtotext/v1/jobs), poll its status
// (GET /speechtotext/v1/jobs/{id}), then fetch the transcript
// (GET /speechtotext/v1/jobs/{id}/transcript).
package revai

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Config holds configuration for the Rev.ai provider.
type Config struct {
	// APIKey is the Rev.ai API key. Falls back to REVAI_API_KEY.
	APIKey string

	// BaseURL is the base URL for the Rev.ai API (optional).
	BaseURL string
}

// Provider represents the Rev.ai provider.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a new Rev.ai provider instance.
func New(config Config) *Provider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.rev.ai"
	}
	if config.APIKey == "" {
		config.APIKey = os.Getenv("REVAI_API_KEY")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: config.BaseURL,
		Headers: map[string]string{
			"Authorization": "Bearer " + config.APIKey,
		},
	})

	return &Provider{
		config: config,
		client: client,
	}
}

// Client returns the HTTP client for making API requests.
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "revai"
}

// LanguageModel returns a language model (not supported by Rev.ai).
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("revai does not provide language models")
}

// EmbeddingModel returns an embedding model (not supported by Rev.ai).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("revai does not provide embedding models")
}

// ImageModel returns an image model (not supported by Rev.ai).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("revai does not provide image models")
}

// SpeechModel returns a speech model (not supported by Rev.ai).
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("revai does not provide speech synthesis models")
}

// TranscriptionModel returns a speech-to-text model.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return &TranscriptionModel{provider: p, modelID: modelID}, nil
}

// RerankingModel returns a reranking model (not supported by Rev.ai).
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("revai does not provide reranking models")
}
