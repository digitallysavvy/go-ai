package mantle

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

func TestCreateBedrockMantleDefaultsAndModels(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{
		Region: "us-west-2",
		APIKey: "bearer",
	})
	if p.Name() != "bedrock-mantle" {
		t.Fatalf("Name = %q", p.Name())
	}
	chat, err := p.Chat(ModelOpenAIGPTOSS20B)
	if err != nil {
		t.Fatalf("Chat error = %v", err)
	}
	if chat.ModelID() != ModelOpenAIGPTOSS20B || chat.Provider() != "bedrock-mantle.chat" {
		t.Fatalf("chat metadata = %s/%s", chat.Provider(), chat.ModelID())
	}
	responses, err := p.Responses(ResponsesModelOpenAIGPTOSS120B)
	if err != nil {
		t.Fatalf("Responses error = %v", err)
	}
	if responses.ModelID() != ResponsesModelOpenAIGPTOSS120B {
		t.Fatalf("responses model = %q", responses.ModelID())
	}
}

func TestBedrockMantleBearerTokenSkipsSigV4Transport(t *testing.T) {
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) { return nil, nil })
	client := &http.Client{Transport: base}
	p := CreateBedrockMantle(ProviderSettings{Region: "us-east-1", APIKey: "bearer", HTTPClient: client})
	if _, ok := p.httpClient.Transport.(*sigV4Transport); ok {
		t.Fatal("bearer token auth must not install SigV4 transport")
	}
}

func TestBedrockMantleRequiresRegionLikeTS(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "us-east-2")

	p := CreateBedrockMantle(ProviderSettings{APIKey: "bearer"})
	if _, err := p.Chat("anthropic.claude"); err == nil {
		t.Fatal("expected Chat to reject missing region")
	}
	if _, err := p.Responses("anthropic.claude"); err == nil {
		t.Fatal("expected Responses to reject missing region")
	}
}

func TestBedrockMantleSigV4FallbackTransport(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{
		Region:          "us-east-1",
		AccessKeyID:     "akid",
		SecretAccessKey: "secret",
	})
	if _, ok := p.httpClient.Transport.(*sigV4Transport); !ok {
		t.Fatal("expected SigV4 transport when bearer token is absent")
	}
}

func TestBedrockMantleCredentialProviderWinsOverStaticCredentials(t *testing.T) {
	var signedRequest *http.Request
	calls := 0
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		signedRequest = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	p := CreateBedrockMantle(ProviderSettings{
		Region:          "us-east-1",
		AccessKeyID:     "static-key",
		SecretAccessKey: "static-secret",
		SessionToken:    "static-session",
		CredentialProvider: func(ctx context.Context) (bedrock.Credentials, error) {
			calls++
			return bedrock.Credentials{
				AccessKeyID:     "dynamic-key",
				SecretAccessKey: "dynamic-secret",
				SessionToken:    "dynamic-session",
			}, nil
		},
		HTTPClient: &http.Client{Transport: base},
	})
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = resp.Body.Close()
	if calls != 1 {
		t.Fatalf("credential provider calls = %d, want 1", calls)
	}
	if signedRequest == nil {
		t.Fatal("base transport was not called")
	}
	if got := signedRequest.Header.Get("X-Amz-Security-Token"); got != "dynamic-session" {
		t.Fatalf("X-Amz-Security-Token = %q, want dynamic-session", got)
	}
	if auth := signedRequest.Header.Get("Authorization"); !strings.Contains(auth, "Credential=dynamic-key/") {
		t.Fatalf("Authorization = %q, want dynamic credential key", auth)
	}
}

func TestBedrockMantleExplicitCredentialsUseEnvSessionTokenLikeTS(t *testing.T) {
	t.Setenv("AWS_SESSION_TOKEN", "env-session")

	var signedRequest *http.Request
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		signedRequest = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	p := CreateBedrockMantle(ProviderSettings{
		Region:          "us-east-1",
		AccessKeyID:     "akid",
		SecretAccessKey: "secret",
		HTTPClient:      &http.Client{Transport: base},
	})
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = resp.Body.Close()
	if signedRequest == nil {
		t.Fatal("base transport was not called")
	}
	if got := signedRequest.Header.Get("X-Amz-Security-Token"); got != "env-session" {
		t.Fatalf("X-Amz-Security-Token = %q, want env-session", got)
	}
}

func TestBedrockMantleUnsupportedModelsReturnNoSuchModelError(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{Region: "us-east-1", APIKey: "bearer"})

	if _, err := p.EmbeddingModel("invalid-model-id"); !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("EmbeddingModel error = %v, want ErrModelNotFound", err)
	}
	if _, err := p.ImageModel("invalid-model-id"); !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("ImageModel error = %v, want ErrModelNotFound", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBaseURLForModel_RoutesOpenAIOnlyModelsToOpenAIPath(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{Region: "us-east-1", APIKey: "bearer"})

	tests := []struct {
		modelID string
		want    string
	}{
		{"openai.gpt-4o-mantle", "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"openai.gpt-5-mantle", "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"google.gemma-4-27b", "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"xai.grok-4", "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		// gpt-oss-* models are excluded from the OpenAI-only route.
		{ModelOpenAIGPTOSS20B, "https://bedrock-mantle.us-east-1.api.aws/v1"},
		{ModelOpenAIGPTOSS120B, "https://bedrock-mantle.us-east-1.api.aws/v1"},
		{"anthropic.claude-sonnet-5", "https://bedrock-mantle.us-east-1.api.aws/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got, err := p.baseURLForModel(tt.modelID)
			if err != nil {
				t.Fatalf("baseURLForModel error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("baseURLForModel(%q) = %q, want %q", tt.modelID, got, tt.want)
			}
		})
	}
}

// TestBaseURLForModel_RejectsRegionThatWouldRewriteHost ports TS
// bedrock-mantle-provider's region validation (TS #21842): region is
// interpolated directly into the request host
// (https://bedrock-mantle.{region}.api.aws/...), so a value that isn't a
// single DNS label must be rejected before any request is sent.
func TestBaseURLForModel_RejectsRegionThatWouldRewriteHost(t *testing.T) {
	for _, region := range []string{"user@internal:8080/#", "evil.example.com/#", "us east 1"} {
		t.Run(region, func(t *testing.T) {
			p := CreateBedrockMantle(ProviderSettings{Region: region, APIKey: "bearer"})
			_, err := p.baseURLForModel("anthropic.claude-sonnet-5")
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("baseURLForModel(region=%q) error = %v, want InvalidArgumentError", region, err)
			}
			if invalid.Field != "region" {
				t.Fatalf("Field = %q, want region", invalid.Field)
			}
		})
	}
}

// TestBaseURLForModel_DoesNotValidateUnusedRegionWithCustomBaseURL ports TS
// "does not validate an unused region with an explicit endpoint": region is
// irrelevant once BaseURL is set, so a garbage value must not block the
// request.
func TestBaseURLForModel_DoesNotValidateUnusedRegionWithCustomBaseURL(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{
		Region:  "user@internal:8080/#",
		APIKey:  "bearer",
		BaseURL: "https://custom.example.com/v1",
	})
	got, err := p.baseURLForModel("anthropic.claude-sonnet-5")
	if err != nil {
		t.Fatalf("baseURLForModel error = %v, want success (region unused with explicit BaseURL)", err)
	}
	if got != "https://custom.example.com/v1" {
		t.Fatalf("baseURLForModel = %q, want explicit override", got)
	}
}

func TestBaseURLForModel_ExplicitBaseURLAlwaysWins(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{Region: "us-east-1", APIKey: "bearer", BaseURL: "https://custom.example.com/v1"})
	got, err := p.baseURLForModel("openai.gpt-4o-mantle")
	if err != nil {
		t.Fatalf("baseURLForModel error = %v", err)
	}
	if got != "https://custom.example.com/v1" {
		t.Fatalf("baseURLForModel = %q, want explicit override", got)
	}
}
