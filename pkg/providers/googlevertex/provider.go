package googlevertex

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const (
	// DefaultBaseURL is the default Google Vertex AI API base URL template
	// {region} and {project} will be replaced with actual values
	DefaultBaseURLTemplate = "https://{region}-aiplatform.googleapis.com/v1beta1/projects/{project}/locations/{region}/publishers/google"
)

// Provider implements the provider.Provider interface for Google Vertex AI
type Provider struct {
	config Config
	client *http.Client
}

// Config contains configuration for the Google Vertex AI provider
type Config struct {
	// Project is the Google Cloud project ID
	Project string

	// Location is the Google Cloud location (e.g., "us-central1")
	Location string

	// AccessToken is the OAuth2 access token for authentication
	// This can be obtained from Google Cloud SDK or service account
	AccessToken string

	// BaseURL is the base URL for the Vertex AI API (optional, computed from project/location if not provided)
	BaseURL string
}

// New creates a new Google Vertex AI provider with the given configuration
func New(cfg Config) (*Provider, error) {
	// Validate required fields
	if cfg.Project == "" {
		return nil, fmt.Errorf("project is required for Google Vertex AI")
	}
	if cfg.Location == "" {
		return nil, fmt.Errorf("location is required for Google Vertex AI")
	}
	if cfg.AccessToken == "" {
		return nil, fmt.Errorf("access token is required for Google Vertex AI")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		// Build base URL from template
		baseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google",
			cfg.Location, cfg.Project, cfg.Location)
	}

	// Google Vertex uses Bearer token authentication
	client := http.NewClient(http.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + cfg.AccessToken,
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}, nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "google-vertex"
}

// LanguageModel returns a language model by ID
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	// For now, return not implemented
	// This would need to be implemented similar to google provider
	return nil, fmt.Errorf("language models not yet implemented for Google Vertex AI")
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return nil, fmt.Errorf("embedding models not yet implemented for Google Vertex AI")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Google Vertex AI does not support speech synthesis through this API")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Google Vertex AI does not support transcription through this API")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Google Vertex AI does not support reranking")
}

// VideoModel returns a video generation model by ID
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	// Validate model ID
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	return nil, fmt.Errorf("video models not yet implemented for Google Vertex AI")
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *http.Client {
	return p.client
}

// Project returns the Google Cloud project ID
func (p *Provider) Project() string {
	return p.config.Project
}

// Location returns the Google Cloud location
func (p *Provider) Location() string {
	return p.config.Location
}
