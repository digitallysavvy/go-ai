package anthropicaws

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

const (
	DefaultAPIVersion = anthropic.DefaultAPIVersion
	serviceName       = "aws-external-anthropic"
)

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type CredentialProvider func(context.Context) (Credentials, error)

type Config struct {
	Region             string
	WorkspaceID        string
	APIKey             string
	AccessKeyID        string
	SecretAccessKey    string
	SessionToken       string
	BaseURL            string
	Headers            map[string]string  `json:"headers,omitempty"`
	HTTPClient         *http.Client       `json:"-"`
	CredentialProvider CredentialProvider `json:"-"`
}

type Provider struct {
	config    Config
	inner     *anthropic.Provider
	region    string
	baseURL   string
	transport *sigV4Transport
}

func New(cfg Config) (*Provider, error) {
	region := firstNonEmpty(cfg.Region, os.Getenv("AWS_REGION"))
	if cfg.BaseURL == "" && region == "" {
		return nil, fmt.Errorf("anthropicaws: AWS region is required; set Config.Region or AWS_REGION")
	}
	workspaceID := firstNonEmpty(cfg.WorkspaceID, os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID"))
	if workspaceID == "" {
		return nil, fmt.Errorf("anthropicaws: workspaceId is required; set Config.WorkspaceID or ANTHROPIC_AWS_WORKSPACE_ID")
	}

	baseURL := strings.TrimSuffix(strings.TrimRight(cfg.BaseURL, "/"), "/v1")
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://aws-external-anthropic.%s.api.aws", region)
	}

	headers := internalhttp.MergeHeaders(map[string]string{
		"anthropic-version":      DefaultAPIVersion,
		"anthropic-workspace-id": workspaceID,
	}, cfg.Headers)

	apiKey := firstNonEmpty(cfg.APIKey, os.Getenv("ANTHROPIC_AWS_API_KEY"))
	httpClient := cfg.HTTPClient
	var transport *sigV4Transport
	if apiKey != "" {
		headers["x-api-key"] = apiKey
	} else {
		transport = &sigV4Transport{
			base:               baseRoundTripper(httpClient),
			region:             region,
			accessKeyID:        cfg.AccessKeyID,
			secretAccessKey:    cfg.SecretAccessKey,
			sessionToken:       cfg.SessionToken,
			credentialProvider: cfg.CredentialProvider,
		}
		httpClient = cloneHTTPClient(httpClient)
		httpClient.Transport = transport
	}

	inner := anthropic.New(anthropic.Config{
		Name:       "anthropic-aws.messages",
		BaseURL:    baseURL,
		Headers:    headers,
		HTTPClient: httpClient,
	})

	return &Provider{config: cfg, inner: inner, region: region, baseURL: baseURL, transport: transport}, nil
}

func CreateAnthropicAws(cfg Config) (*Provider, error) {
	return New(cfg)
}

func (p *Provider) Name() string { return "anthropic-aws.messages" }

func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	return p.inner.LanguageModel(modelID)
}

func (p *Provider) LanguageModelWithOptions(modelID string, options *anthropic.ModelOptions) (provider.LanguageModel, error) {
	return p.inner.LanguageModelWithOptions(modelID, options)
}

func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return nil, fmt.Errorf("anthropicaws does not support embedding models")
}

func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return nil, fmt.Errorf("anthropicaws does not support image generation")
}

func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	return nil, fmt.Errorf("anthropicaws does not support speech synthesis")
}

func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	return nil, fmt.Errorf("anthropicaws does not support transcription")
}

func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	return nil, fmt.Errorf("anthropicaws does not support reranking")
}

func (p *Provider) Files() provider.FilesAPI {
	return p.inner.Files()
}

func (p *Provider) Skills() provider.SkillsAPI {
	return p.inner.Skills()
}

var Tools = anthropictools.AnthropicTools

func (p *Provider) BaseURL() string { return p.baseURL }

func (p *Provider) LastSigningCredentials() Credentials {
	if p.transport == nil {
		return Credentials{}
	}
	return p.transport.lastCredentials
}

type sigV4Transport struct {
	base               http.RoundTripper
	region             string
	accessKeyID        string
	secretAccessKey    string
	sessionToken       string
	credentialProvider CredentialProvider
	lastCredentials    Credentials
}

func (t *sigV4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))

	creds, err := t.credentials(req.Context())
	if err != nil {
		return nil, err
	}
	t.lastCredentials = creds
	signAWSRequest(req, body, t.region, creds)
	req.Body = io.NopCloser(bytes.NewReader(body))
	return t.base.RoundTrip(req)
}

func (t *sigV4Transport) credentials(ctx context.Context) (Credentials, error) {
	if t.credentialProvider != nil {
		creds, err := t.credentialProvider(ctx)
		if err != nil {
			return Credentials{}, fmt.Errorf("AWS credential provider failed: %v. Please ensure your credential provider returns valid AWS credentials with accessKeyId and secretAccessKey properties", err)
		}
		if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
			return Credentials{}, fmt.Errorf("AWS credential provider failed: missing accessKeyId or secretAccessKey. Please ensure your credential provider returns valid AWS credentials with accessKeyId and secretAccessKey properties")
		}
		return creds, nil
	}

	accessKeyID := firstNonEmpty(t.accessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
	secretAccessKey := firstNonEmpty(t.secretAccessKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
	sessionToken := firstNonEmpty(t.sessionToken, os.Getenv("AWS_SESSION_TOKEN"))
	if accessKeyID == "" {
		return Credentials{}, fmt.Errorf("AWS SigV4 authentication requires AWS credentials. Please provide either:\n1. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables\n2. Provide AccessKeyID and SecretAccessKey in Config\n3. Use a CredentialProvider function\n4. Use API key authentication with ANTHROPIC_AWS_API_KEY or Config.APIKey")
	}
	if secretAccessKey == "" {
		return Credentials{}, fmt.Errorf("AWS SigV4 authentication requires both AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. Please ensure both credentials are provided")
	}
	return Credentials{AccessKeyID: accessKeyID, SecretAccessKey: secretAccessKey, SessionToken: sessionToken}, nil
}

func signAWSRequest(req *http.Request, payload []byte, region string, creds Credentials) {
	now := time.Now().UTC()
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	canonicalRequest := canonicalRequest(req, payload)
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", now.Format("20060102"), region, serviceName)
	hash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", now.Format("20060102T150405Z"), scope, hex.EncodeToString(hash[:]))
	signature := signingSignature(creds.SecretAccessKey, now, region, stringToSign)
	signedHeaders := signedHeaders(req.Header)
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", creds.AccessKeyID, scope, signedHeaders, signature))
}

func canonicalRequest(req *http.Request, payload []byte) string {
	payloadHash := sha256.Sum256(payload)
	canonicalHeaders, signed := canonicalHeaders(req.Header)
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.Join([]string{req.Method, path, req.URL.RawQuery, canonicalHeaders, signed, hex.EncodeToString(payloadHash[:])}, "\n")
}

func canonicalHeaders(headers http.Header) (string, string) {
	keys := make([]string, 0, len(headers))
	values := map[string]string{}
	for k, vals := range headers {
		lower := strings.ToLower(k)
		keys = append(keys, lower)
		if len(vals) > 0 {
			values[lower] = strings.TrimSpace(vals[0])
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(":")
		b.WriteString(values[k])
		b.WriteString("\n")
	}
	return b.String(), strings.Join(keys, ";")
}

func signedHeaders(headers http.Header) string {
	_, signed := canonicalHeaders(headers)
	return signed
}

func signingSignature(secret string, now time.Time, region, stringToSign string) string {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(now.Format("20060102")))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(serviceName))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func baseRoundTripper(client *http.Client) http.RoundTripper {
	if client != nil && client.Transport != nil {
		return client.Transport
	}
	return http.DefaultTransport
}

func cloneHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{}
	}
	clone := *client
	return &clone
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
