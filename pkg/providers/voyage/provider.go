package voyage

import (
	"context"
	"fmt"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"net/http"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const DefaultBaseURL = "https://api.voyageai.com/v1"

type Config struct {
	APIKey string
	// APIKeyFunc resolves the key per request instead of APIKey (TS
	// `apiKey: Resolvable<string>`, 7716e189ee). The environment is not
	// read when it is set; setting both is an InvalidArgumentError. The func
	// gets the request's context.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`
	BaseURL    string
	Headers    map[string]string `json:"headers,omitempty"`
	HTTPClient *http.Client      `json:"-"`
}

type Provider struct {
	config Config
	client *internalhttp.Client
}

func New(cfg Config) *Provider {
	// TS withoutTrailingSlash(baseURL): a trailing "/" must not double up with request paths.
	cfg.BaseURL = providerutils.WithoutTrailingSlash(cfg.BaseURL)
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	apiKey := cfg.APIKey
	if apiKey == "" && cfg.APIKeyFunc == nil {
		apiKey = os.Getenv("VOYAGE_API_KEY")
	}
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", apiKey),
	}
	return &Provider{
		config: cfg,
		client: internalhttp.NewClient(internalhttp.Config{
			BaseURL:    baseURL,
			Headers:    version.WithUserAgentSuffix(internalhttp.MergeHeaders(headers, cfg.Headers), version.ProviderUserAgent("voyage")),
			HTTPClient: cfg.HTTPClient,
			// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
			HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
				if cfg.APIKeyFunc != nil {
					return providerutils.RequestAuthHeaderFunc(ctx, cfg.Headers, providerutils.ResolveAPIKeyOptions{
						APIKey: cfg.APIKey, APIKeyFunc: cfg.APIKeyFunc, EnvVar: "VOYAGE_API_KEY", Description: "Voyage",
					}, "Authorization", "Bearer ")
				}
				return providerutils.RequestAuthHeader(cfg.Headers, apiKey, "VOYAGE_API_KEY", "Voyage", "Authorization", "Bearer ")
			},
		}),
	}
}

func CreateVoyage(cfg Config) *Provider { return New(cfg) }
func (p *Provider) Name() string        { return "voyage" }

func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "languageModel")
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
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
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
