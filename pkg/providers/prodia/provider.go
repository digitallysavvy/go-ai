package prodia

import (
	"fmt"
	"os"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Provider implements the provider.Provider interface for Prodia
type Provider struct {
	config Config
	client *internalhttp.Client
}

// Config contains configuration for the Prodia provider
type Config struct {
	// APIKey is the Prodia API token.
	// If empty, the PRODIA_TOKEN environment variable is used.
	APIKey string

	// BaseURL is the base URL for the Prodia API (optional).
	// Defaults to https://inference.prodia.com/v2
	BaseURL string
}

// New creates a new Prodia provider with the given configuration.
// If Config.APIKey is empty, the API key is loaded from the PRODIA_TOKEN
// environment variable.
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://inference.prodia.com/v2"
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("PRODIA_TOKEN")
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
			"Content-Type":  "application/json",
		},
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "prodia"
}

// effectiveBaseURL returns the resolved base URL, applying the default when
// Config.BaseURL was left empty.
func (p *Provider) effectiveBaseURL() string {
	if p.config.BaseURL != "" {
		return p.config.BaseURL
	}
	return "https://inference.prodia.com/v2"
}

// effectiveAPIKey returns the API key, falling back to the PRODIA_TOKEN
// environment variable when Config.APIKey was not set.
func (p *Provider) effectiveAPIKey() string {
	if p.config.APIKey != "" {
		return p.config.APIKey
	}
	return os.Getenv("PRODIA_TOKEN")
}

// LanguageModel returns a language model by ID.
// Routes inference.nano-banana.* model IDs to ProdiaLanguageModel.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = LanguageModelNanoBananaImgToImgV2
	}
	if strings.HasPrefix(modelID, "inference.nano-banana.") {
		return NewLanguageModel(p, modelID), nil
	}
	return nil, fmt.Errorf("prodia: unsupported language model %q", modelID)
}

// VideoModel returns a video generation model by ID.
// Routes inference.wan2-2.* model IDs to ProdiaVideoModel.
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = VideoModelWan22LightningTxt2Vid
	}
	if strings.HasPrefix(modelID, "inference.wan2-2.") {
		return NewVideoModel(p, modelID), nil
	}
	return nil, fmt.Errorf("prodia: unsupported video model %q", modelID)
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("prodia does not support embeddings")
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = "inference.flux-fast.schnell.txt2img.v2"
	}
	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("prodia does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("prodia does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("prodia does not support reranking")
}
