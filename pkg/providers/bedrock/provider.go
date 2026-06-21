package bedrock

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Provider implements the provider.Provider interface for AWS Bedrock
type Provider struct {
	config Config
	client *internalhttp.Client

	credMu    sync.Mutex
	creds     awsCredentials
	credsFrom string
	credExp   time.Time
}

// Config contains configuration for the AWS Bedrock provider
type Config struct {
	// APIKey authenticates requests with Bedrock bearer token authentication.
	// Defaults to AWS_BEARER_TOKEN_BEDROCK. When set, it takes precedence over SigV4 credentials.
	APIKey string

	// AWSAccessKeyID is the AWS access key ID
	AWSAccessKeyID string

	// AWSSecretAccessKey is the AWS secret access key
	AWSSecretAccessKey string

	// Region is the AWS region (e.g., "us-east-1")
	Region string

	// BaseURL overrides Bedrock runtime and agent-runtime API calls.
	BaseURL string

	// SessionToken is an optional AWS session token for temporary credentials
	SessionToken string

	// CredentialProvider returns dynamic AWS credentials for SigV4 signing.
	// When set, it takes precedence over AWSAccessKeyID, AWSSecretAccessKey,
	// SessionToken, environment variables, shared credentials, and instance roles.
	CredentialProvider CredentialProvider `json:"-"`

	// SharedCredentialsFile overrides the shared credentials file path.
	// Defaults to AWS_SHARED_CREDENTIALS_FILE, then ~/.aws/credentials.
	SharedCredentialsFile string

	// Profile selects the shared credentials profile.
	// Defaults to AWS_PROFILE, then "default".
	Profile string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// Credentials contains AWS authentication information for Bedrock SigV4 signing.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
}

// CredentialProvider returns dynamic AWS credentials for SigV4 authentication.
type CredentialProvider func(context.Context) (Credentials, error)

type awsCredentials = Credentials

func (c Credentials) valid() bool {
	return c.AccessKeyID != "" && c.SecretAccessKey != ""
}

// New creates a new AWS Bedrock provider with the given configuration
func New(cfg Config) *Provider {
	if cfg.Region == "" {
		cfg.Region = os.Getenv("AWS_REGION")
	}

	// AWS Bedrock endpoint
	baseURL := cfg.BaseURL
	if baseURL == "" && cfg.Region != "" {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", cfg.Region)
	}

	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: baseURL,
		Headers: version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
			"Content-Type": "application/json",
		}, cfg.Headers), version.ProviderUserAgent("amazon-bedrock")),
	})

	return &Provider{
		config: cfg,
		client: client,
	}
}

// CreateAmazonBedrock creates a new AWS Bedrock provider.
//
// It mirrors the TypeScript SDK createAmazonBedrock export while New remains
// the idiomatic Go constructor.
func CreateAmazonBedrock(cfg Config) *Provider {
	return New(cfg)
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "amazon-bedrock"
}

// LanguageModel returns a language model by ID
// Model IDs for Bedrock look like: "anthropic.claude-3-sonnet-20240229-v1:0"
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewLanguageModel(p, modelID), nil
}

// LanguageModelWithOptions returns a language model with custom options
func (p *Provider) LanguageModelWithOptions(modelID string, options *ModelOptions) (provider.LanguageModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewLanguageModel(p, modelID, options), nil
}

// EmbeddingModel returns an embedding model by ID with default options
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewEmbeddingModel(p, modelID), nil
}

// EmbeddingModelWithOptions returns an embedding model by ID with custom options
func (p *Provider) EmbeddingModelWithOptions(modelID string, options *EmbeddingOptions) (provider.EmbeddingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock")
	}

	return NewEmbeddingModel(p, modelID, options), nil
}

// ImageModel returns an image generation model by ID
func (p *Provider) ImageModel(modelID string) (provider.ImageModel, error) {
	return NewImageModel(p, modelID), nil
}

// SpeechModel returns a speech synthesis model by ID
func (p *Provider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	// Bedrock doesn't provide TTS models directly
	return nil, fmt.Errorf("AWS Bedrock does not support speech synthesis")
}

// TranscriptionModel returns a speech-to-text model by ID
func (p *Provider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	// Bedrock doesn't provide STT models directly
	return nil, fmt.Errorf("AWS Bedrock does not support transcription")
}

// RerankingModel returns a reranking model by ID
func (p *Provider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model ID is required for AWS Bedrock reranking")
	}
	return NewRerankingModel(p, modelID), nil
}

// Client returns the HTTP client for making API requests
func (p *Provider) Client() *internalhttp.Client {
	return p.client
}

func (p *Provider) applyRequestHeaders(req *stdhttp.Request, overrides map[string]string) {
	headers := map[string]string{}
	for k, values := range req.Header {
		if len(values) > 0 {
			headers[k] = values[0]
		}
	}
	for k, v := range p.config.Headers {
		headers[k] = v
	}
	for k, v := range overrides {
		headers[k] = v
	}
	headers = version.WithUserAgentSuffix(headers, version.ProviderUserAgent("amazon-bedrock"))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// Region returns the AWS region
func (p *Provider) Region() string {
	return p.config.Region
}

func (p *Provider) runtimeBaseURL() (string, error) {
	if p.config.BaseURL != "" {
		return strings.TrimRight(p.config.BaseURL, "/"), nil
	}
	if p.config.Region == "" {
		return "", fmt.Errorf("AWS region is required: set Region or AWS_REGION")
	}
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", p.config.Region), nil
}

func (p *Provider) agentRuntimeBaseURL() (string, error) {
	if p.config.BaseURL != "" {
		return strings.TrimRight(p.config.BaseURL, "/"), nil
	}
	if p.config.Region == "" {
		return "", fmt.Errorf("AWS region is required: set Region or AWS_REGION")
	}
	return fmt.Sprintf("https://bedrock-agent-runtime.%s.amazonaws.com", p.config.Region), nil
}

func (p *Provider) authenticateRequest(ctx context.Context, req *stdhttp.Request, payload []byte) error {
	apiKey := strings.TrimSpace(firstNonEmpty(p.config.APIKey, os.Getenv("AWS_BEARER_TOKEN_BEDROCK")))
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return nil
	}

	creds, err := p.resolveCredentials(ctx)
	if err != nil {
		return err
	}
	if p.config.Region == "" {
		return fmt.Errorf("AWS region is required: set Region or AWS_REGION")
	}

	signer := NewAWSSigner(
		creds.AccessKeyID,
		creds.SecretAccessKey,
		creds.SessionToken,
		p.config.Region,
	)
	if err := signer.SignRequest(req, payload); err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}
	return nil
}

func (p *Provider) resolveCredentials(ctx context.Context) (awsCredentials, error) {
	p.credMu.Lock()
	defer p.credMu.Unlock()

	if p.config.CredentialProvider != nil {
		creds, err := p.config.CredentialProvider(ctx)
		if err != nil {
			return awsCredentials{}, fmt.Errorf("AWS credential provider failed: %v. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields", err)
		}
		if !creds.valid() {
			return awsCredentials{}, fmt.Errorf("AWS credential provider failed: incomplete credentials. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields")
		}
		p.credsFrom = "credential-provider"
		return creds, nil
	}

	now := time.Now()
	if p.creds.valid() && (p.credExp.IsZero() || now.Before(p.credExp.Add(-5*time.Minute))) {
		return p.creds, nil
	}

	if p.config.AWSAccessKeyID != "" || p.config.AWSSecretAccessKey != "" || p.config.SessionToken != "" {
		creds := awsCredentials{
			AccessKeyID:     firstNonEmpty(p.config.AWSAccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID")),
			SecretAccessKey: firstNonEmpty(p.config.AWSSecretAccessKey, os.Getenv("AWS_SECRET_ACCESS_KEY")),
			SessionToken:    p.config.SessionToken,
		}
		if creds.SessionToken == "" && (p.config.AWSAccessKeyID == "" || p.config.AWSSecretAccessKey == "") {
			creds.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
		}
		if !creds.valid() {
			return awsCredentials{}, fmt.Errorf("incomplete AWS credentials in Bedrock config")
		}
		p.creds, p.credsFrom, p.credExp = creds, "config", time.Time{}
		return creds, nil
	}
	if creds := envCredentials(); creds.valid() {
		p.creds, p.credsFrom, p.credExp = creds, "env", creds.Expiration
		return creds, nil
	}
	if creds, err := sharedFileCredentials(p.config.SharedCredentialsFile, p.config.Profile); err == nil && creds.valid() {
		p.creds, p.credsFrom, p.credExp = creds, "shared-credentials", creds.Expiration
		return creds, nil
	} else if err != nil && p.config.SharedCredentialsFile != "" {
		return awsCredentials{}, err
	}
	if creds, err := instanceRoleCredentials(ctx); err == nil && creds.valid() {
		p.creds, p.credsFrom, p.credExp = creds, "instance-role", creds.Expiration
		return creds, nil
	}

	return awsCredentials{}, fmt.Errorf("AWS credentials not found: set AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, Bedrock Config credentials, shared credentials file, or an instance role")
}

func envCredentials() awsCredentials {
	return awsCredentials{
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sharedFileCredentials(path, profile string) (awsCredentials, error) {
	if path == "" {
		path = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return awsCredentials{}, err
		}
		path = filepath.Join(home, ".aws", "credentials")
	}
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return awsCredentials{}, nil
		}
		return awsCredentials{}, err
	}
	defer file.Close() //nolint:errcheck

	values := map[string]map[string]string{}
	current := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if values[current] == nil {
				values[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[current][strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return awsCredentials{}, err
	}

	section := values[profile]
	return awsCredentials{
		AccessKeyID:     section["aws_access_key_id"],
		SecretAccessKey: section["aws_secret_access_key"],
		SessionToken:    section["aws_session_token"],
	}, nil
}

func instanceRoleCredentials(ctx context.Context) (awsCredentials, error) {
	client := &stdhttp.Client{Timeout: 2 * time.Second}
	tokenReq, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPut, "http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		return awsCredentials{}, err
	}
	tokenReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return awsCredentials{}, err
	}
	tokenBytes, err := readAndClose(tokenResp)
	if err != nil {
		return awsCredentials{}, err
	}
	if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
		return awsCredentials{}, fmt.Errorf("IMDS token request failed with status %d", tokenResp.StatusCode)
	}
	token := string(tokenBytes)

	roleReq, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, "http://169.254.169.254/latest/meta-data/iam/security-credentials/", nil)
	if err != nil {
		return awsCredentials{}, err
	}
	roleReq.Header.Set("X-aws-ec2-metadata-token", token)
	roleResp, err := client.Do(roleReq)
	if err != nil {
		return awsCredentials{}, err
	}
	roleBytes, err := readAndClose(roleResp)
	if err != nil {
		return awsCredentials{}, err
	}
	if roleResp.StatusCode < 200 || roleResp.StatusCode >= 300 {
		return awsCredentials{}, fmt.Errorf("IMDS role request failed with status %d", roleResp.StatusCode)
	}
	roleName := strings.TrimSpace(string(roleBytes))
	if roleName == "" {
		return awsCredentials{}, fmt.Errorf("IMDS returned empty role name")
	}

	credReq, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, "http://169.254.169.254/latest/meta-data/iam/security-credentials/"+roleName, nil)
	if err != nil {
		return awsCredentials{}, err
	}
	credReq.Header.Set("X-aws-ec2-metadata-token", token)
	credResp, err := client.Do(credReq)
	if err != nil {
		return awsCredentials{}, err
	}
	credBytes, err := readAndClose(credResp)
	if err != nil {
		return awsCredentials{}, err
	}
	if credResp.StatusCode < 200 || credResp.StatusCode >= 300 {
		return awsCredentials{}, fmt.Errorf("IMDS credential request failed with status %d", credResp.StatusCode)
	}

	var payload struct {
		AccessKeyID     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		Token           string `json:"Token"`
		Expiration      string `json:"Expiration"`
	}
	if err := json.Unmarshal(credBytes, &payload); err != nil {
		return awsCredentials{}, err
	}
	exp, _ := time.Parse(time.RFC3339, payload.Expiration)
	return awsCredentials{
		AccessKeyID:     payload.AccessKeyID,
		SecretAccessKey: payload.SecretAccessKey,
		SessionToken:    payload.Token,
		Expiration:      exp,
	}, nil
}

func readAndClose(resp *stdhttp.Response) ([]byte, error) {
	defer resp.Body.Close() //nolint:errcheck
	return io.ReadAll(resp.Body)
}
