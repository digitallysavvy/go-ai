package googlevertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// maasMaxOutputTokensByModel mirrors TS maxOutputTokensByModel
// (google-vertex-maas-provider.ts): some MaaS-hosted models silently truncate
// output at a low default unless max_tokens is set explicitly.
var maasMaxOutputTokensByModel = map[string]int{
	"meta/llama-4-maverick-17b-128e-instruct-maas": 8192,
	"meta/llama-4-scout-17b-16e-instruct-maas":     8192,
}

// applyMaasMaxTokensDefault ports TS transformGoogleVertexMaasRequestBody:
// when the request targets a model in maasMaxOutputTokensByModel and does
// not already set max_tokens, inject the model's default. Any error, or a
// request body that isn't a JSON object, leaves body unchanged.
func applyMaasMaxTokensDefault(body []byte) []byte {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	modelID, _ := m["model"].(string)
	maxTokens, ok := maasMaxOutputTokensByModel[modelID]
	if !ok {
		return body
	}
	if _, exists := m["max_tokens"]; exists {
		return body
	}
	m["max_tokens"] = maxTokens
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

type HeadersResolver func(ctx context.Context) (map[string]string, error)

func StaticHeaders(headers map[string]string) HeadersResolver {
	return func(context.Context) (map[string]string, error) {
		if headers == nil {
			return nil, nil
		}
		cloned := make(map[string]string, len(headers))
		for k, v := range headers {
			cloned[k] = v
		}
		return cloned, nil
	}
}

type GoogleAuthOptions struct {
	TokenSource oauth2.TokenSource
}

type MaaSConfig struct {
	Project   string
	Location  string
	BaseURL   string
	Headers   HeadersResolver
	AuthToken func(ctx context.Context) (string, error)

	// Fetch customizes the underlying HTTP transport, matching the TS fetch option.
	Fetch *stdhttp.Client

	// GoogleAuthOptions customizes Google auth token resolution, matching the TS Node option.
	GoogleAuthOptions *GoogleAuthOptions
}

// MaaSProvider provides access to Vertex AI Model-as-a-Service models.
type MaaSProvider struct {
	config MaaSConfig

	mu       sync.Mutex
	provider *openai.Provider
	initErr  error
	options  maasProviderOptions
}

type maasProviderOptions struct {
	headersFunc func(ctx context.Context) (map[string]string, error)
	authToken   func(ctx context.Context) (string, error)
	httpClient  *stdhttp.Client
}

type maasAuthTransport struct {
	base        stdhttp.RoundTripper
	headersFunc func(ctx context.Context) (map[string]string, error)
	authToken   func(ctx context.Context) (string, error)
}

func (t *maasAuthTransport) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	clone := req.Clone(req.Context())
	if clone.Body != nil && clone.Method == stdhttp.MethodPost {
		bodyBytes, err := io.ReadAll(clone.Body)
		_ = clone.Body.Close()
		if err != nil {
			return nil, err
		}
		bodyBytes = applyMaasMaxTokensDefault(bodyBytes)
		clone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		clone.ContentLength = int64(len(bodyBytes))
	}
	if t.headersFunc != nil {
		headers, err := t.headersFunc(req.Context())
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			clone.Header.Set(k, v)
		}
	}
	token, err := t.authToken(req.Context())
	if err != nil {
		return nil, err
	}
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(clone)
}

func NewMaaS(config MaaSConfig) *MaaSProvider {
	return newMaaS(config, maasProviderOptions{})
}

func newMaaS(config MaaSConfig, options maasProviderOptions) *MaaSProvider {
	return &MaaSProvider{config: config, options: options}
}

func (p *MaaSProvider) Name() string { return "vertex.maas" }

func (p *MaaSProvider) init() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.provider != nil || p.initErr != nil {
		return p.initErr
	}

	baseURL := strings.TrimRight(p.config.BaseURL, "/")
	if baseURL == "" {
		project := p.config.Project
		if project == "" {
			project = os.Getenv("GOOGLE_VERTEX_PROJECT")
		}
		if project == "" {
			p.initErr = fmt.Errorf("project is required for Google Vertex MaaS")
			return p.initErr
		}

		location := p.config.Location
		if location == "" {
			location = os.Getenv("GOOGLE_VERTEX_LOCATION")
		}
		if location == "" {
			location = "global"
		}
		// Location is interpolated directly into the request host, so only a
		// single DNS label is accepted; ports TS
		// google-vertex-maas-provider.ts (TS #21842).
		if !providerutils.IsValidHostnamePart(location) {
			p.initErr = &providererrors.InvalidArgumentError{
				Field:   "location",
				Message: "Invalid Google Vertex location. Expected a single DNS label (letters, digits, and hyphens). Use `BaseURL` for custom endpoints.",
			}
			return p.initErr
		}

		baseURL = maasBaseURL(project, location)
	}

	authToken := p.config.AuthToken
	if authToken == nil {
		authToken = p.options.authToken
	}
	if authToken == nil && p.config.GoogleAuthOptions != nil && p.config.GoogleAuthOptions.TokenSource != nil {
		authToken = func(ctx context.Context) (string, error) {
			token, err := p.config.GoogleAuthOptions.TokenSource.Token()
			if err != nil {
				return "", fmt.Errorf("failed to fetch Google access token: %w", err)
			}
			if token == nil || token.AccessToken == "" {
				return "", fmt.Errorf("google token source returned an empty access token")
			}
			return token.AccessToken, nil
		}
	}
	if authToken == nil {
		authToken = defaultMaasAuthToken
	}

	baseTransport := stdhttp.RoundTripper(stdhttp.DefaultTransport)
	httpClientConfig := p.config.Fetch
	if httpClientConfig == nil {
		httpClientConfig = p.options.httpClient
	}
	if httpClientConfig != nil && httpClientConfig.Transport != nil {
		baseTransport = httpClientConfig.Transport
	}
	if transport, ok := baseTransport.(*stdhttp.Transport); ok {
		baseTransport = transport.Clone()
	}
	var httpClient *stdhttp.Client
	if httpClientConfig != nil {
		clone := *httpClientConfig
		httpClient = &clone
	} else {
		httpClient = &stdhttp.Client{}
	}
	headersFunc := p.config.Headers
	if headersFunc == nil {
		headersFunc = p.options.headersFunc
	}
	httpClient.Transport = &maasAuthTransport{
		base:        baseTransport,
		headersFunc: headersFunc,
		authToken:   authToken,
	}

	p.provider = openai.New(openai.Config{
		Name:             "vertex.maas",
		BaseURL:          baseURL,
		HTTPClient:       httpClient,
		ChatProviderName: "vertex.maas",
		// TS google-vertex-maas-provider.ts builds on @ai-sdk/openai-compatible's
		// createOpenAICompatible, which tags requests with its own
		// `ai-sdk/openai-compatible/VERSION` (not `ai-sdk/google-vertex`).
		UserAgentName: "openai-compatible",
	})
	return nil
}

func maasBaseURL(project, location string) string {
	return fmt.Sprintf("https://%s/v1/projects/%s/locations/%s/endpoints/openapi", vertexHost(location), project, location)
}

// defaultMaasTokenCache caches the ADC access token used by
// defaultMaasAuthToken, mirroring the main googlevertex.Provider's own
// cachedTokenSource (provider.go) -- and the TS SDK's equivalent
// createAuthTokenGenerator, which hands back a single `GoogleAuth` client
// closed over once and reused for the life of the provider, relying on the
// google-auth-library's own internal token caching rather than fetching a
// fresh token on every call. Without this, defaultMaasAuthToken previously
// called google.DefaultTokenSource(ctx, ...).Token() fresh on every single
// outgoing HTTP request (via maasAuthTransport.RoundTrip), which re-reads/
// re-parses credentials and can trip GCE metadata-server rate limits under a
// busy workload. Package-scoped (not per-MaaSProvider) because
// defaultMaasAuthToken is itself a free function with no provider receiver.
var defaultMaasTokenCache = &cachedTokenSource{
	resolve: func(ctx context.Context) (string, time.Time, error) {
		tokenSource, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return "", time.Time{}, fmt.Errorf("failed to load Google application default credentials: %w", err)
		}
		token, err := tokenSource.Token()
		if err != nil {
			return "", time.Time{}, fmt.Errorf("failed to fetch Google access token: %w", err)
		}
		if token == nil || token.AccessToken == "" {
			return "", time.Time{}, fmt.Errorf("google application default credentials returned an empty access token")
		}
		return token.AccessToken, token.Expiry, nil
	},
}

func defaultMaasAuthToken(ctx context.Context) (string, error) {
	if token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN"); token != "" {
		return token, nil
	}
	return defaultMaasTokenCache.tokenFor(ctx)
}

func (p *MaaSProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return p.provider.ChatModel(modelID)
}

func (p *MaaSProvider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

func (p *MaaSProvider) CompletionModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

func (p *MaaSProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return p.provider.EmbeddingModel(modelID)
}

func (p *MaaSProvider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

func (p *MaaSProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return p.provider.ImageModel(modelID)
}

func (p *MaaSProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("Google Vertex MaaS does not support speech synthesis") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

func (p *MaaSProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Google Vertex MaaS does not support transcription") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

func (p *MaaSProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Google Vertex MaaS does not support reranking") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
}

// VertexMaaS is the default Google Vertex MaaS provider instance.
var VertexMaaS = NewMaaS(MaaSConfig{})
