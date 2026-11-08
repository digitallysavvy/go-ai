// Package gladia provides a Gladia AI speech-to-text provider for the Go AI SDK.
// Gladia offers advanced speech recognition with async processing and multi-language support.
package gladia

import (
	"strings"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Config holds configuration for the Gladia provider
type Config struct {
	// API key for authentication
	APIKey string

	// Base URL for the Gladia API (optional, defaults to production)
	BaseURL string

	// Headers are custom HTTP headers sent with every request (TS provider
	// settings `headers`).
	Headers map[string]string `json:"headers,omitempty"`

	// PollInterval is the wait between transcription job status checks (TS
	// provider setting pollIntervalMs). Zero means the default, 1 second.
	// It must be a whole number of milliseconds; a negative or fractional
	// value fails the call before the upload with an InvalidArgumentError
	// (TS throws it from createGladia).
	PollInterval time.Duration `json:"pollInterval,omitempty"`

	// PollTimeout is the longest wait for an asynchronous transcription job
	// to finish before a TranscriptionJobPollingTimedOut error (TS provider
	// setting pollTimeoutMs). Zero means the default, 10 minutes. Raise it
	// for long audio or audio intelligence add-ons. Same validation as
	// PollInterval. The per-call provider options pollIntervalMs /
	// pollTimeoutMs override both.
	PollTimeout time.Duration `json:"pollTimeout,omitempty"`
}

// validatePollingSettings is TS validatePollingSettings: each set polling
// setting must be a positive whole number of milliseconds.
func validatePollingSettings(config Config) error {
	for _, setting := range []struct {
		argument string
		value    time.Duration
	}{{"pollIntervalMs", config.PollInterval}, {"pollTimeoutMs", config.PollTimeout}} {
		if setting.value == 0 {
			continue
		}
		if setting.value < 0 || setting.value%time.Millisecond != 0 {
			return providererrors.NewArgumentError(setting.argument, setting.argument+" must be a positive integer number of milliseconds", nil)
		}
	}
	return nil
}

// Provider represents the Gladia AI provider
type Provider struct {
	config Config
	client *internalhttp.Client
}

// New creates a new Gladia provider instance
func New(config Config) *Provider {
	// TS loadApiKey: the key defaults to the GLADIA_API_KEY environment variable.
	config.APIKey = providerutils.APIKeyOrEnv(config.APIKey, "GLADIA_API_KEY")
	if config.BaseURL == "" {
		config.BaseURL = "https://api.gladia.io/v2"
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: config.BaseURL,
		// TS c8875b71a3: x-gladia-version goes out with the provider
		// headers, before the caller's headers so a caller header wins.
		Headers: version.WithUserAgentSuffix(mergeHeadersFold(map[string]string{
			"x-gladia-key":     config.APIKey,
			"x-gladia-version": version.SDKUserAgent(),
		}, config.Headers), version.ProviderUserAgent("gladia")),
		// TS loadApiKey runs per request: a missing key is a LoadAPIKeyError.
		HeadersFunc: func() (map[string]string, error) {
			return providerutils.RequestAuthHeader(config.Headers, config.APIKey, "GLADIA_API_KEY", "Gladia", "x-gladia-key", "")
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

// Name returns the provider name
func (p *Provider) Name() string {
	return "gladia"
}

// LanguageModel returns a language model (not supported by Gladia)
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "languageModel", Message: "Gladia does not provide language models"}
}

// EmbeddingModel returns an embedding model (not supported by Gladia)
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "embeddingModel", Message: "Gladia does not provide embedding models"}
}

// ImageModel returns an image model (not supported by Gladia)
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, &providererrors.NoSuchModelError{ModelID: modelID, ModelType: "imageModel", Message: "Gladia does not provide image models"}
}

// SpeechModel returns a speech synthesis model (not supported by Gladia)
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a speech-to-text model
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return &TranscriptionModel{
		provider: p,
		modelID:  modelID,
	}, nil
}

// RerankingModel returns a reranking model (not supported by Gladia)
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// mergeHeadersFold merges header maps left to right like TS object spread
// over Headers: a later name replaces an earlier one regardless of case, so
// a caller's X-Gladia-Version wins over the default x-gladia-version.
func mergeHeadersFold(headers ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, h := range headers {
		for k, v := range h {
			for existing := range merged {
				if existing != k && strings.EqualFold(existing, k) {
					delete(merged, existing)
				}
			}
			merged[k] = v
		}
	}
	return merged
}
