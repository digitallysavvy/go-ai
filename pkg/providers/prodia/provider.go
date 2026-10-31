package prodia

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"os"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
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

	// Headers contains custom headers to include in all requests.
	Headers map[string]string
}

// New creates a new Prodia provider with the given configuration.
// If Config.APIKey is empty, the API key is loaded from the PRODIA_TOKEN
// environment variable.
func New(cfg Config) *Provider {
	// TS withoutTrailingSlash(baseURL): a trailing "/" must not double up with request paths.
	cfg.BaseURL = providerutils.WithoutTrailingSlash(cfg.BaseURL)
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://inference.prodia.com/v2"
	}

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("PRODIA_TOKEN")
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(headers, version.ProviderUserAgent("prodia")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			return providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "PRODIA_TOKEN", "Prodia", "Authorization", "Bearer ")
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

// effectiveAPIKey loads the API key like TS loadApiKey: Config.APIKey, else
// the PRODIA_TOKEN environment variable, else a LoadAPIKeyError. A caller
// that supplies its own Authorization header keeps it (empty key, no error).
func (p *Provider) effectiveAPIKey() (string, error) {
	h, err := providerutils.RequestAuthHeader(p.config.Headers, p.config.APIKey, "PRODIA_TOKEN", "Prodia", "Authorization", "Bearer ")
	if err != nil || h == nil {
		return "", err
	}
	return strings.TrimPrefix(h["Authorization"], "Bearer "), nil
}

// LanguageModel returns a language model by ID. Any ID is accepted (TS
// ProdiaLanguageModelId is `'inference.nano-banana.img2img.v2' | (string & {})`),
// so new Prodia job types work without an SDK release.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = LanguageModelNanoBananaImgToImgV2
	}
	return NewLanguageModel(p, modelID), nil
}

// VideoModel returns a video generation model by ID. Any ID is accepted (TS
// ProdiaVideoModelId ends in `(string & {})`).
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	if modelID == "" {
		modelID = VideoModelWan22LightningTxt2Vid
	}
	return NewVideoModel(p, modelID), nil
}

// EmbeddingModel returns an embedding model by ID
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
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
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}
