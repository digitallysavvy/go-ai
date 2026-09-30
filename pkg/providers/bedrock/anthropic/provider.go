// Package anthropic implements the Bedrock-Anthropic provider: the native
// Anthropic Messages API surfaced through AWS Bedrock's InvokeModel /
// InvokeModelWithResponseStream endpoints (as opposed to the Bedrock Converse
// API used by pkg/providers/bedrock).
//
// This is a thin wrapper around pkg/providers/anthropic.LanguageModel, mirroring
// pkg/providers/googlevertex/anthropic and TS createAmazonBedrockAnthropic
// (amazon-bedrock-anthropic-provider.ts): the Bedrock-specific request/response
// transforms are supplied via anthropic.Config's Transform* hooks so that the
// shared Anthropic prompt converter, capability table and streaming state
// machine are reused as-is. This also fixes a review finding in the previous
// standalone implementation: in-prompt system messages were silently dropped
// whenever Prompt.System was also set, and converter betas/warnings were
// never propagated (see the wg_b2_system_prompt_test.go proof).
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	anthropicprovider "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	bedrock "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

const (
	// AnthropicVersion is the Bedrock Anthropic API version, sent as
	// anthropic_version in the request body (Bedrock does not use the
	// anthropic-version HTTP header).
	AnthropicVersion = "bedrock-2023-05-31"

	// providerName is reported by Name(), matching TS 'bedrock.anthropic.messages'.
	providerName = "bedrock.anthropic.messages"
)

// AWSCredentials contains AWS authentication information for SigV4 signing.
type AWSCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string // Optional for temporary credentials
}

// Config for the Bedrock Anthropic provider.
type Config struct {
	// Region is the AWS region (e.g., "us-east-1"). Defaults to AWS_REGION.
	Region string

	// Credentials for AWS SigV4 authentication. If nil, environment
	// variables are used, unless BearerToken/CredentialProvider are set.
	Credentials *AWSCredentials

	// CredentialProvider returns dynamic AWS credentials for SigV4 signing.
	// When set, it takes precedence over Credentials and environment variables.
	CredentialProvider bedrock.CredentialProvider

	// BearerToken enables API-key authentication instead of SigV4. Defaults
	// to the AWS_BEARER_TOKEN_BEDROCK environment variable. Takes precedence
	// over SigV4 credentials when non-empty.
	BearerToken string

	// BaseURL overrides the default Bedrock endpoint
	// (default: https://bedrock-runtime.{region}.amazonaws.com).
	BaseURL string

	// Headers are custom HTTP headers to include in every request.
	Headers map[string]string

	// HTTPClient allows custom HTTP client configuration. Its Transport (or
	// http.DefaultTransport) is wrapped with SigV4/bearer-token
	// authentication; do not set Transport expecting it to see unsigned
	// requests.
	HTTPClient *http.Client
}

// BedrockAnthropicProvider implements native Anthropic Messages API access
// via AWS Bedrock.
type BedrockAnthropicProvider struct {
	config Config
	region string
}

// New creates a new Bedrock Anthropic provider.
func New(config Config) *BedrockAnthropicProvider {
	region := firstNonEmpty(config.Region, os.Getenv("AWS_REGION"))
	return &BedrockAnthropicProvider{config: config, region: region}
}

// CreateAmazonBedrockAnthropic mirrors the TypeScript SDK's
// createAmazonBedrockAnthropic export.
func CreateAmazonBedrockAnthropic(config Config) *BedrockAnthropicProvider {
	return New(config)
}

// Tools exposes the Anthropic-specific tool factories (TS provider.tools).
var Tools = anthropictools.AnthropicTools

// Name returns the provider name.
func (p *BedrockAnthropicProvider) Name() string { return providerName }

// runtimeBaseURL resolves the Bedrock runtime endpoint using the shared
// partition-aware resolver (pkg/providers/bedrock.ResolveAmazonBedrockBaseURL),
// so Bedrock-Anthropic gets the same precedence (explicit BaseURL, then
// AWS_ENDPOINT_URL_BEDROCK_RUNTIME, then AWS_ENDPOINT_URL, then a generated
// URL with the correct partition DNS suffix) as the Converse client.
func (p *BedrockAnthropicProvider) runtimeBaseURL() (string, error) {
	return bedrock.ResolveAmazonBedrockBaseURL(bedrock.ResolveBaseURLOptions{
		BaseURL:                              p.config.BaseURL,
		Region:                               p.region,
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
}

// LanguageModel returns a language model by ID.
func (p *BedrockAnthropicProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModelWithOptions(modelID, nil)
}

// LanguageModelWithOptions returns a language model with Anthropic
// provider-specific settings (the same ModelOptions surface as the direct
// Anthropic provider, e.g. Thinking, Safeguards, CacheControl/AutomaticCaching).
func (p *BedrockAnthropicProvider) LanguageModelWithOptions(modelID string, options *anthropicprovider.ModelOptions) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID cannot be empty")
	}

	baseURL, err := p.runtimeBaseURL()
	if err != nil {
		return nil, err
	}

	nativeStructuredOutput := supportsNativeStructuredOutput(modelID)
	strictTools := supportsStrictTools(modelID)
	supportsImageInput := true

	inner := anthropicprovider.New(anthropicprovider.Config{
		Name:                           providerName,
		BaseURL:                        baseURL,
		Headers:                        p.config.Headers,
		HTTPClient:                     p.httpClient(),
		OmitAPIVersionHeader:           true, // anthropic_version goes in the body, not a header.
		SupportsNativeStructuredOutput: &nativeStructuredOutput,
		SupportsStrictTools:            &strictTools,
		SupportsImageInput:             &supportsImageInput,
		// TS amazon-bedrock-anthropic-provider.ts tags requests
		// "ai-sdk/amazon-bedrock/VERSION" (its own package's tag, shared with
		// the Converse-API amazon-bedrock provider), not "ai-sdk/anthropic".
		UserAgentName: "amazon-bedrock",
		// Bedrock-Anthropic forces base64 conversion instead of passing URLs
		// through, matching TS amazon-bedrock-anthropic-provider.ts
		// (`supportedUrls: () => ({})`).
		SupportedURLs: func(string) map[string][]string { return map[string][]string{} },
		MessagesPath: func(id string, stream bool) string {
			action := "invoke"
			if stream {
				action = "invoke-with-response-stream"
			}
			return fmt.Sprintf("/model/%s/%s", encodeURIComponent(id), action)
		},
		TransformRequestBodyWithBetas: transformRequestBodyWithBetas,
		TransformStreamBody:           transformEventStreamToSSE,
		TransformErrorBody:            transformErrorBody,
	})

	return anthropicprovider.NewLanguageModel(inner, modelID, options), nil
}

// EmbeddingModel returns an embedding model (not supported).
func (p *BedrockAnthropicProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, noSuchModelError(modelID, "embeddingModel")
}

// ImageModel returns an image model (not supported).
func (p *BedrockAnthropicProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, noSuchModelError(modelID, "imageModel")
}

// SpeechModel returns a speech model (not supported).
func (p *BedrockAnthropicProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support speech models")
}

// TranscriptionModel returns a transcription model (not supported).
func (p *BedrockAnthropicProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support transcription models")
}

// RerankingModel returns a reranking model (not supported).
func (p *BedrockAnthropicProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("bedrock-anthropic provider does not support reranking models")
}

func noSuchModelError(modelID, modelType string) error {
	return fmt.Errorf("%w: bedrock-anthropic %s %q", providererrors.ErrModelNotFound, modelType, modelID)
}

// --- authentication ---------------------------------------------------------

// httpClient wraps the configured (or default) HTTP client with a transport
// that authenticates every request: bearer-token if configured, otherwise
// AWS SigV4.
func (p *BedrockAnthropicProvider) httpClient() *http.Client {
	base := p.config.HTTPClient
	if base == nil {
		base = &http.Client{}
	}
	clone := *base
	baseTransport := clone.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	clone.Transport = &bedrockAnthropicTransport{base: baseTransport, provider: p}
	return &clone
}

// bedrockAnthropicTransport authenticates outgoing requests and forces the
// correct Accept header for the streaming invoke action (the shared
// anthropic.LanguageModel sets "text/event-stream", but Bedrock's
// invoke-with-response-stream endpoint returns
// application/vnd.amazon.eventstream regardless).
type bedrockAnthropicTransport struct {
	base     http.RoundTripper
	provider *BedrockAnthropicProvider
}

func (t *bedrockAnthropicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "invoke-with-response-stream") {
		req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	}

	bearerToken := strings.TrimSpace(firstNonEmpty(t.provider.config.BearerToken, os.Getenv("AWS_BEARER_TOKEN_BEDROCK")))
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
		return t.base.RoundTrip(req)
	}

	if req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))

	creds, err := t.provider.resolveCredentials(req.Context())
	if err != nil {
		return nil, err
	}

	signer := bedrock.NewAWSSigner(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, t.provider.region)
	if err := signer.SignRequest(req, body); err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return t.base.RoundTrip(req)
}

// resolveCredentials resolves SigV4 credentials: CredentialProvider first,
// then explicit Config.Credentials, then environment variables. The ambient
// AWS_SESSION_TOKEN is only used as a fallback when the caller did not
// explicitly supply both an access key and a secret key (TS row 6732c16;
// matches pkg/providers/bedrock.Provider's resolveCredentials).
func (p *BedrockAnthropicProvider) resolveCredentials(ctx context.Context) (AWSCredentials, error) {
	if p.config.CredentialProvider != nil {
		creds, err := p.config.CredentialProvider(ctx)
		if err != nil {
			return AWSCredentials{}, fmt.Errorf("AWS credential provider failed: %v. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields", err)
		}
		if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
			return AWSCredentials{}, fmt.Errorf("AWS credential provider failed: incomplete credentials. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields")
		}
		return AWSCredentials{AccessKeyID: creds.AccessKeyID, SecretAccessKey: creds.SecretAccessKey, SessionToken: creds.SessionToken}, nil
	}

	var explicitAccessKeyID, explicitSecretAccessKey, explicitSessionToken string
	if p.config.Credentials != nil {
		explicitAccessKeyID = p.config.Credentials.AccessKeyID
		explicitSecretAccessKey = p.config.Credentials.SecretAccessKey
		explicitSessionToken = p.config.Credentials.SessionToken
	}

	accessKeyID := firstNonEmpty(explicitAccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
	secretAccessKey := firstNonEmpty(explicitSecretAccessKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
	sessionToken := explicitSessionToken
	if sessionToken == "" && (explicitAccessKeyID == "" || explicitSecretAccessKey == "") {
		sessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}

	if accessKeyID == "" {
		return AWSCredentials{}, fmt.Errorf("AWS SigV4 authentication requires AWS credentials. Please provide either:\n" +
			"1. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables\n" +
			"2. Provide Credentials in Config\n" +
			"3. Use a CredentialProvider function\n" +
			"4. Use API key authentication with BearerToken or AWS_BEARER_TOKEN_BEDROCK")
	}
	if secretAccessKey == "" {
		return AWSCredentials{}, fmt.Errorf("AWS SigV4 authentication requires both AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. Please ensure both credentials are provided")
	}

	return AWSCredentials{AccessKeyID: accessKeyID, SecretAccessKey: secretAccessKey, SessionToken: sessionToken}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// encodeURIComponent mirrors JavaScript's encodeURIComponent, which Go's
// url.PathEscape does not: encodeURIComponent percent-encodes ':' and '/'
// (both allowed, unencoded, in a URL path segment per RFC 3986), which
// matters for Bedrock model IDs/ARNs such as
// "anthropic.claude-3-sonnet-20240229-v1:0".
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isURIComponentUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isURIComponentUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}
	return false
}
