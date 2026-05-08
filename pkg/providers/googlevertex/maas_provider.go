package googlevertex

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

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

		baseURL = fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/%s/endpoints/openapi", project, location)
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
		Name:       "vertex.maas",
		BaseURL:    baseURL,
		HTTPClient: httpClient,
	})
	return nil
}

func defaultMaasAuthToken(ctx context.Context) (string, error) {
	if token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN"); token != "" {
		return token, nil
	}
	tokenSource, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("failed to load Google application default credentials: %w", err)
	}
	token, err := tokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("failed to fetch Google access token: %w", err)
	}
	if token == nil || token.AccessToken == "" {
		return "", fmt.Errorf("google application default credentials returned an empty access token")
	}
	return token.AccessToken, nil
}

func (p *MaaSProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}
	if err := p.init(); err != nil {
		return nil, err
	}
	return p.provider.LanguageModel(modelID)
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
	return nil, fmt.Errorf("Google Vertex MaaS does not support speech synthesis")
}

func (p *MaaSProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("Google Vertex MaaS does not support transcription")
}

func (p *MaaSProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("Google Vertex MaaS does not support reranking")
}

// VertexMaaS is the default Google Vertex MaaS provider instance.
var VertexMaaS = NewMaaS(MaaSConfig{})
