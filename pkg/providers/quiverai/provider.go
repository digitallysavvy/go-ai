package quiverai

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const defaultBaseURL = "https://api.quiver.ai/v1"

// Config contains configuration for the QuiverAI provider.
type Config struct {
	APIKey  string            `json:"apiKey,omitempty"`
	BaseURL string            `json:"baseURL,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Provider implements SVG generation and vectorization for QuiverAI.
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a QuiverAI provider. APIKey and BaseURL fall back to
// QUIVERAI_API_KEY and QUIVERAI_BASE_URL to mirror the TypeScript SDK.
func New(cfg Config) *Provider {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("QUIVERAI_API_KEY")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("QUIVERAI_BASE_URL")
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	headers := internalhttp.MergeHeaders(map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
		"User-Agent":    "go-ai/quiverai",
	}, cfg.Headers)
	return &Provider{
		config: cfg,
		client: internalhttp.NewClient(internalhttp.Config{
			BaseURL: baseURL,
			Headers: headers,
		}),
	}
}

// CreateQuiverAI mirrors the TypeScript createQuiverAI export.
func CreateQuiverAI(cfg Config) *Provider {
	return New(cfg)
}

func (p *Provider) Name() string { return "quiverai" }

func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("quiverai does not support language models")
}

func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("quiverai does not support embeddings")
}

func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		modelID = ModelArrow11
	}
	return NewImageModel(p, modelID), nil
}

func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("quiverai does not support speech synthesis")
}

func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("quiverai does not support transcription")
}

func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("quiverai does not support reranking")
}

func (p *Provider) Client() *internalhttp.Client { return p.client }
