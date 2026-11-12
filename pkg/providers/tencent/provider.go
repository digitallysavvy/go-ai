package tencent

import (
	"context"
	"math"
	stdhttp "net/http"
	"os"
	"strconv"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DefaultBaseURL is the TokenHub base URL (TS defaultTokenHubBaseURL).
const DefaultBaseURL = "https://tokenhub-intl.tencentcloudmaas.com/v1"

// DefaultVodBaseURL is the VOD API base URL (TS defaultVodBaseURL).
const DefaultVodBaseURL = "https://vod.intl.tencentcloudapi.com"

const (
	apiKeyEnvVar       = "TENCENT_TOKENHUB_API_KEY"
	apiKeyDescription  = "Tencent TokenHub API key"
	secretIDEnvVar     = "TENCENTCLOUD_SECRET_ID"
	secretKeyEnvVar    = "TENCENTCLOUD_SECRET_KEY"
	sessionTokenEnvVar = "TENCENTCLOUD_SESSION_TOKEN"
	subAppIDEnvVar     = "TENCENTCLOUD_VOD_SUB_APP_ID"
)

// Config contains configuration for the Tencent Cloud provider (TS
// TencentProviderSettings).
type Config struct {
	// APIKey is the TokenHub API key (sk-...), used by language models and
	// TokenHub video models. Defaults to the TENCENT_TOKENHUB_API_KEY
	// environment variable, read per request.
	APIKey string

	// APIKeyFunc resolves the TokenHub API key for every request instead of
	// APIKey (TS `apiKey` as a Resolvable). Its error fails the request; the
	// environment is not read. Setting both APIKey and APIKeyFunc fails
	// every request with an InvalidArgumentError.
	APIKeyFunc providerutils.APIKeyFunc `json:"-"`

	// BaseURL is the TokenHub base URL. Defaults to
	// https://tokenhub-intl.tencentcloudmaas.com/v1.
	BaseURL string

	// SecretID is the Tencent Cloud API SecretId, used by VOD video models.
	// Defaults to the TENCENTCLOUD_SECRET_ID environment variable.
	SecretID string `json:"-"`

	// SecretKey is the Tencent Cloud API SecretKey, used by VOD video
	// models. Defaults to the TENCENTCLOUD_SECRET_KEY environment variable.
	SecretKey string `json:"-"`

	// SessionToken is the session token of temporary Tencent Cloud
	// credentials (sent as X-TC-Token). Defaults to the
	// TENCENTCLOUD_SESSION_TOKEN environment variable; not needed with a
	// long-term SecretId / SecretKey pair.
	SessionToken string `json:"-"`

	// SubAppID is the VOD application ID (SubAppId), used by VOD video
	// models: a positive integer, given as its decimal digits (TS number |
	// string). Defaults to the TENCENTCLOUD_VOD_SUB_APP_ID environment
	// variable.
	SubAppID string

	// Region is the Tencent Cloud region sent as X-TC-Region on VOD
	// requests. VOD AIGC actions do not require a region.
	Region string

	// VodBaseURL is the VOD API base URL. Defaults to
	// https://vod.intl.tencentcloudapi.com.
	VodBaseURL string

	// Headers are custom HTTP headers to include in the requests.
	Headers map[string]string `json:"headers,omitempty"`

	// HTTPClient overrides the HTTP client used for all requests.
	HTTPClient *stdhttp.Client `json:"-"`
}

// Provider implements the provider.Provider interface for Tencent Cloud.
type Provider struct {
	config     Config
	baseURL    string
	vodBaseURL string

	// chat is the OpenAI-compatible TokenHub chat transport (TS
	// TencentChatLanguageModel extends OpenAICompatibleChatLanguageModel).
	chat *openai.Provider

	// tokenHub sends TokenHub video requests with the per-request
	// Authorization header; vod sends signed VOD requests.
	tokenHub *internalhttp.Client
	vod      *internalhttp.Client
}

// New creates a Tencent Cloud provider (TS createTencent). TokenHub-only use
// needs no VOD credentials: they are resolved when a VOD request is made.
func New(cfg Config) *Provider {
	baseURL := providerutils.WithoutTrailingSlash(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	vodBaseURL := providerutils.WithoutTrailingSlash(cfg.VodBaseURL)
	if vodBaseURL == "" {
		vodBaseURL = DefaultVodBaseURL
	}
	userAgent := version.ProviderUserAgent("tencent")

	chat := openai.New(openai.Config{
		APIKey:            cfg.APIKey,
		APIKeyFunc:        cfg.APIKeyFunc,
		APIKeyEnvVar:      apiKeyEnvVar,
		APIKeyDescription: apiKeyDescription,
		BaseURL:           baseURL,
		Headers:           cfg.Headers,
		HTTPClient:        cfg.HTTPClient,
		Name:              "tencent",
		ChatProviderName:  "tencent.chat",
		// TS TencentChatLanguageModel is an OpenAICompatibleChatLanguageModel
		// with includeUsage: true and the default compatible error
		// structure; supportsStructuredOutputs keeps its default (false).
		OpenAICompatible:          true,
		IncludeUsage:              true,
		SupportsStructuredOutputs: new(bool),
		CompatErrorStructure:      providerutils.OpenAICompatibleAPIError,
		UserAgentName:             "tencent",
	})

	userHeaders, userKey, keyFunc := cfg.Headers, cfg.APIKey, cfg.APIKeyFunc
	tokenHub := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    baseURL,
		Headers:    version.WithUserAgentSuffix(internalhttp.MergeHeaders(cfg.Headers), userAgent),
		HTTPClient: cfg.HTTPClient,
		// TS getTokenHubHeaders: loadApiKey runs per request, so a missing
		// key is a LoadAPIKeyError.
		HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
			if keyFunc != nil {
				return providerutils.RequestAuthHeaderFunc(ctx, userHeaders, providerutils.ResolveAPIKeyOptions{
					APIKey: userKey, APIKeyFunc: keyFunc, EnvVar: apiKeyEnvVar, Description: apiKeyDescription,
				}, "Authorization", "Bearer ")
			}
			return providerutils.RequestAuthHeader(userHeaders, userKey, apiKeyEnvVar, apiKeyDescription, "Authorization", "Bearer ")
		},
	})
	// TS getVodHeaders: the caller headers with the user agent suffix; the
	// TC3 protocol headers are added per request.
	vod := internalhttp.NewClient(internalhttp.Config{
		BaseURL:    vodBaseURL,
		Headers:    version.WithUserAgentSuffix(internalhttp.MergeHeaders(cfg.Headers), userAgent),
		HTTPClient: cfg.HTTPClient,
	})

	return &Provider{
		config:     cfg,
		baseURL:    baseURL,
		vodBaseURL: vodBaseURL,
		chat:       chat,
		tokenHub:   tokenHub,
		vod:        vod,
	}
}

// CreateTencent creates a Tencent Cloud provider. It mirrors the TS
// createTencent export while New remains the idiomatic Go constructor.
func CreateTencent(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "tencent"
}

// LanguageModel returns a TokenHub chat model (TS provider(modelId) and
// provider.languageModel).
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.chat.ChatModel(modelID)
}

// ChatModel returns a TokenHub chat model (TS provider.chat).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// VideoModel returns a video model (TS provider.video / videoModel).
// TokenHub Kling models use their TokenHub ID (e.g. "kling-video-v3"); VOD
// AIGC models use "vod/<ModelName>/<ModelVersion>" (e.g. "vod/VS/2.0"). Any
// other ID is a NoSuchModelError.
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error) {
	m, err := NewVideoModel(p, modelID)
	if err != nil {
		return nil, err // no typed-nil interface
	}
	return m, nil
}

// EmbeddingModel returns a NoSuchModelError: Tencent has no embedding model
// factory (TS throws NoSuchModelError).
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "embeddingModel")
}

// TextEmbeddingModel is the deprecated alias of EmbeddingModel (TS
// textEmbeddingModel).
//
// Deprecated: use EmbeddingModel.
func (p *Provider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// ImageModel returns a NoSuchModelError (TS throws NoSuchModelError).
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "imageModel")
}

// SpeechModel returns a NoSuchModelError: TS createTencent has no speech
// model factory.
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "speechModel")
}

// TranscriptionModel returns a NoSuchModelError: TS createTencent has no
// transcription model factory.
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "transcriptionModel")
}

// RerankingModel returns a NoSuchModelError: TS createTencent has no
// reranking model factory.
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, providererrors.NewNoSuchModelError(modelID, "rerankingModel")
}

// vodCredentials are the TC3 signing credentials and the VOD application ID
// (TS TencentVodCredentials).
type vodCredentials struct {
	secretID     string
	secretKey    string
	sessionToken string
	subAppID     int64
}

// vodCredentials resolves the VOD credentials for one request (TS
// getVodCredentials): each setting falls back to its environment variable,
// a missing one is a LoadSettingError, and the SubAppId must be a positive
// safe integer.
func (p *Provider) vodCredentials() (vodCredentials, error) {
	subAppID := p.config.SubAppID
	subAppIDSet := subAppID != ""
	if !subAppIDSet {
		subAppID = os.Getenv(subAppIDEnvVar)
		if subAppID == "" {
			return vodCredentials{}, providererrors.NewLoadSettingError("Tencent VOD SubAppId", "subAppId", subAppIDEnvVar)
		}
	}
	secretID, err := loadSetting(p.config.SecretID, "secretId", secretIDEnvVar, "Tencent Cloud SecretId")
	if err != nil {
		return vodCredentials{}, err
	}
	secretKey, err := loadSetting(p.config.SecretKey, "secretKey", secretKeyEnvVar, "Tencent Cloud SecretKey")
	if err != nil {
		return vodCredentials{}, err
	}
	sessionToken := p.config.SessionToken
	if sessionToken == "" {
		sessionToken = os.Getenv(sessionTokenEnvVar)
	}
	parsed, err := parseSubAppID(subAppID)
	if err != nil {
		return vodCredentials{}, err
	}
	return vodCredentials{secretID: secretID, secretKey: secretKey, sessionToken: sessionToken, subAppID: parsed}, nil
}

// loadSetting is TS loadSetting: the explicit value, else the environment
// variable, else a LoadSettingError.
func loadSetting(value, settingName, envVar, description string) (string, error) {
	if value != "" {
		return value, nil
	}
	if env := os.Getenv(envVar); env != "" {
		return env, nil
	}
	return "", providererrors.NewLoadSettingError(description, settingName, envVar)
}

// parseSubAppID is TS parseSubAppId: Number(value.trim()) must be a positive
// safe integer.
func parseSubAppID(value string) (int64, error) {
	f, ok := providerutils.JSNumber(providerutils.JSTrim(value))
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f <= 0 || f > 9007199254740991 {
		return 0, &providererrors.LoadSettingError{Message: "Tencent VOD SubAppId must be a positive integer."}
	}
	// strconv round-trips the safe integer exactly.
	n, err := strconv.ParseInt(strconv.FormatFloat(f, 'f', 0, 64), 10, 64)
	if err != nil {
		return 0, &providererrors.LoadSettingError{Message: "Tencent VOD SubAppId must be a positive integer."}
	}
	return n, nil
}
