package voyage

import (
	"fmt"
	"net/http"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const DefaultBaseURL = "https://api.voyageai.com/v1"

type Config struct {
	APIKey     string
	BaseURL    string
	Headers    map[string]string `json:"headers,omitempty"`
	HTTPClient *http.Client      `json:"-"`
}

type Provider struct {
	config Config
	client *internalhttp.Client
}

func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("VOYAGE_API_KEY")
	}
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", apiKey),
	}
	return &Provider{
		config: cfg,
		client: internalhttp.NewClient(internalhttp.Config{
			BaseURL:    baseURL,
			Headers:    internalhttp.MergeHeaders(headers, cfg.Headers),
			HTTPClient: cfg.HTTPClient,
		}),
	}
}

func CreateVoyage(cfg Config) *Provider { return New(cfg) }
func (p *Provider) Name() string        { return "voyage" }

func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, fmt.Errorf("voyage does not support language models")
}

func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewEmbeddingModel(p, modelID), nil
}

func (p *Provider) Embedding(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

func (p *Provider) TextEmbedding(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

func (p *Provider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("voyage does not support image models")
}

func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("voyage does not support speech models")
}

func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("voyage does not support transcription models")
}

func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	return NewRerankingModel(p, modelID), nil
}

func (p *Provider) Reranking(modelID string) (provider.RerankingModel, error) {
	return p.RerankingModel(modelID)
}
