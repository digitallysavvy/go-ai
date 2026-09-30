package quiverai

import (
	"fmt"
	"os"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const defaultBaseURL = "https://api.quiver.ai/v1"

// customToolID identifies QuiverAI's caller-executed custom tool, mirroring
// TS's `customToolId: 'quiverai.custom'` provider setting.
const customToolID = "quiverai.custom"

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

// resolveQuiverAIConfig resolves APIKey/BaseURL against the QUIVERAI_API_KEY
// / QUIVERAI_BASE_URL environment variables and the package default base
// URL, shared by New (image model client) and LanguageModel (Open Responses
// transport) so both use identical credentials/endpoint resolution.
func resolveQuiverAIConfig(cfg Config) (apiKey, baseURL string) {
	apiKey = cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("QUIVERAI_API_KEY")
	}
	baseURL = cfg.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("QUIVERAI_BASE_URL")
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return apiKey, baseURL
}

// New creates a QuiverAI provider. APIKey and BaseURL fall back to
// QUIVERAI_API_KEY and QUIVERAI_BASE_URL to mirror the TypeScript SDK.
func New(cfg Config) *Provider {
	apiKey, baseURL := resolveQuiverAIConfig(cfg)
	headers := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, cfg.Headers), version.ProviderUserAgent("quiverai"))
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

// LanguageModel creates a language model for the QuiverAI Responses API
// (Arrow 2 / Arrow 2 Telos), reusing the Open Responses transport with
// QuiverAI's request policy layered on top. Mirrors the TS SDK's
// createQuiverAI().languageModel.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		modelID = ModelArrow2
	}
	apiKey, baseURL := resolveQuiverAIConfig(p.config)
	structuredOutputsDisabled := false
	orProvider := openresponses.New(openresponses.Config{
		BaseURL:                  baseURL + "/responses",
		APIKey:                   apiKey,
		Headers:                  p.config.Headers,
		Name:                     "quiverai",
		StrictResponseInput:      true,
		CustomToolID:             customToolID,
		StructuredOutputs:        &structuredOutputsDisabled,
		FailedResponseHandler:    quiverAIFailedResponseHandler,
		GetResponseErrorMetadata: quiverAIResponseErrorMetadata,
		UserAgentSuffix:          version.ProviderUserAgent("quiverai"),
	})
	inner, err := orProvider.LanguageModel(modelID)
	if err != nil {
		return nil, err
	}
	return NewLanguageModel(inner, modelID, p.config), nil
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

// Tools returns a factory for QuiverAI's caller-executed tools (currently
// just CustomTool), mirroring the TS SDK's `quiverai.tools` (aliased from
// the underlying Open Responses provider's `tools`).
func (p *Provider) Tools() *openresponses.Tools {
	return openresponses.NewTools(customToolID)
}
