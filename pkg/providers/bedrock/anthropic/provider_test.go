package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProviderName(t *testing.T) {
	p := New(Config{Region: "us-east-1"})
	if got := p.Name(); got != "bedrock.anthropic.messages" {
		t.Errorf("Name() = %q, want bedrock.anthropic.messages", got)
	}
}

func TestLanguageModel_RequiresModelID(t *testing.T) {
	p := New(Config{Region: "us-east-1", BearerToken: "token"})
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected an error for an empty model ID")
	}
}

func TestLanguageModel_RequiresRegionOrBaseURL(t *testing.T) {
	p := New(Config{BearerToken: "token"}) // no Region, no BaseURL
	if _, err := p.LanguageModel("test-model-id"); err == nil {
		t.Fatal("expected an error when neither Region nor BaseURL is set")
	}
}

func TestLanguageModel_CustomBaseURLWithoutRegion(t *testing.T) {
	p := New(Config{BaseURL: "https://custom-bedrock.example.com", BearerToken: "token"})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if model == nil {
		t.Fatal("model is nil")
	}
}

// TestBuildRequestURL_NonStreaming ports "should build correct URL for
// non-streaming requests" — encodeURIComponent must escape ':' (unlike Go's
// url.PathEscape, which leaves ':' unescaped in a path segment).
// TestRuntimeBaseURL_UsesSharedPartitionAwareResolver verifies
// Bedrock-Anthropic now shares pkg/providers/bedrock's
// ResolveAmazonBedrockBaseURL (TS resolve-amazon-bedrock-base-url.ts): it
// picks the correct DNS suffix for non-standard AWS partitions instead of
// always assuming amazonaws.com.
func TestRuntimeBaseURL_UsesSharedPartitionAwareResolver(t *testing.T) {
	p := New(Config{Region: "cn-north-1", BearerToken: "token"})
	got, err := p.runtimeBaseURL()
	if err != nil {
		t.Fatalf("runtimeBaseURL error = %v", err)
	}
	want := "https://bedrock-runtime.cn-north-1.amazonaws.com.cn"
	if got != want {
		t.Fatalf("runtimeBaseURL = %q, want %q", got, want)
	}
}

// TestRuntimeBaseURL_RespectsEndpointEnvVar verifies the shared resolver's
// AWS_ENDPOINT_URL_BEDROCK_RUNTIME / AWS_ENDPOINT_URL support now applies to
// Bedrock-Anthropic too.
func TestRuntimeBaseURL_RespectsEndpointEnvVar(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "https://custom-runtime.example.com/")
	p := New(Config{Region: "us-east-1", BearerToken: "token"})
	got, err := p.runtimeBaseURL()
	if err != nil {
		t.Fatalf("runtimeBaseURL error = %v", err)
	}
	if got != "https://custom-runtime.example.com" {
		t.Fatalf("runtimeBaseURL = %q, want the env var override (trailing slash stripped)", got)
	}
}

func TestBuildRequestURL_NonStreaming(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath (not Path) preserves the %3A the client sent; net/http
		// decodes URL.Path for convenience.
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-3-sonnet-20240229-v1:0")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	want := "/model/anthropic.claude-3-sonnet-20240229-v1%3A0/invoke"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

// TestUserAgentTaggedAmazonBedrockNotAnthropic covers the owner's 2026-09-30
// User-Agent decision: TS amazon-bedrock-anthropic-provider.ts tags requests
// with its own package's "ai-sdk/amazon-bedrock/VERSION" tag (shared with the
// Converse-API amazon-bedrock provider), never "ai-sdk/anthropic" -- even
// though this Go package reuses pkg/providers/anthropic as its transport.
func TestUserAgentTaggedAmazonBedrockNotAnthropic(t *testing.T) {
	var gotUserAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-3-sonnet-20240229-v1:0")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}}); err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !strings.HasPrefix(gotUserAgent, "ai-sdk/amazon-bedrock/") {
		t.Fatalf("User-Agent = %q, want ai-sdk/amazon-bedrock/... prefix", gotUserAgent)
	}
	if strings.Contains(gotUserAgent, "ai-sdk/anthropic/") {
		t.Fatalf("User-Agent = %q, must not carry the ai-sdk/anthropic tag", gotUserAgent)
	}
}

func TestBearerTokenAuthentication(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "sk-bedrock-token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotAuth != "Bearer sk-bedrock-token" {
		t.Errorf("Authorization = %q, want Bearer sk-bedrock-token", gotAuth)
	}
}

// TestStreamingAcceptHeaderOverride verifies the transport forces
// application/vnd.amazon.eventstream on the invoke-with-response-stream path,
// even though the shared Anthropic LM sets "text/event-stream" by default.
func TestStreamingAcceptHeaderOverride(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		// Empty body is fine; the test only inspects the request header.
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err == nil && stream != nil {
		_ = stream.Close()
	}
	if gotAccept != "application/vnd.amazon.eventstream" {
		t.Errorf("Accept = %q, want application/vnd.amazon.eventstream", gotAccept)
	}
}

func TestSigV4Authentication(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{
		Region:  "us-east-1",
		BaseURL: srv.URL,
		Credentials: &AWSCredentials{
			AccessKeyID:     "AKIDEXAMPLE",
			SecretAccessKey: "secret",
		},
		HTTPClient: srv.Client(),
	})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotAuth == "" {
		t.Error("Authorization header should be set by SigV4 signing")
	}
}

func TestSigV4Authentication_MissingCredentialsErrors(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New(Config{Region: "us-east-1", BaseURL: srv.URL, HTTPClient: srv.Client()})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err == nil {
		t.Fatal("expected an error when no credentials are available")
	}
}

// TestSessionTokenEnvFallback_OnlyWithoutExplicitKeys ports row 6732c16: the
// ambient AWS_SESSION_TOKEN must not be picked up when the caller supplies
// both an explicit access key and secret key.
func TestSessionTokenEnvFallback_OnlyWithoutExplicitKeys(t *testing.T) {
	t.Setenv("AWS_SESSION_TOKEN", "env-session-token")

	p := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     "explicit-key",
			SecretAccessKey: "explicit-secret",
		},
	})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.SessionToken != "" {
		t.Errorf("SessionToken = %q, want empty (explicit keys were provided without a session token)", creds.SessionToken)
	}
}

func TestSessionTokenEnvFallback_UsedWhenKeysFromEnv(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_SESSION_TOKEN", "env-session-token")

	p := New(Config{Region: "us-east-1"})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.SessionToken != "env-session-token" {
		t.Errorf("SessionToken = %q, want env-session-token", creds.SessionToken)
	}
}

func TestEmbeddingModelNotSupported(t *testing.T) {
	p := New(Config{Region: "us-east-1"})
	if _, err := p.EmbeddingModel("m"); err == nil {
		t.Error("expected an error for EmbeddingModel")
	}
}

func TestImageModelNotSupported(t *testing.T) {
	p := New(Config{Region: "us-east-1"})
	if _, err := p.ImageModel("m"); err == nil {
		t.Error("expected an error for ImageModel")
	}
}

func TestEncodeURIComponent(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"anthropic.claude-3-sonnet-20240229-v1:0", "anthropic.claude-3-sonnet-20240229-v1%3A0"},
		{"us.anthropic.claude-3-5-sonnet-20240620-v1:0", "us.anthropic.claude-3-5-sonnet-20240620-v1%3A0"},
		{"simple-id", "simple-id"},
		{"arn:aws:bedrock:us-east-1:123:application-inference-profile/foo", "arn%3Aaws%3Abedrock%3Aus-east-1%3A123%3Aapplication-inference-profile%2Ffoo"},
	}
	for _, tt := range tests {
		if got := encodeURIComponent(tt.in); got != tt.want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Ports the `config.supportedUrls?.()` toEqual({}) assertion in
// amazon-bedrock-anthropic-provider.test.ts (ai@7.0.113): Bedrock-Anthropic
// never passes image/PDF URLs through directly.
func TestBedrockAnthropicSupportedURLsForcesBase64Conversion(t *testing.T) {
	p := New(Config{Region: "us-east-1", BearerToken: "token"})
	model, err := p.LanguageModel("anthropic.claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	urlsProvider, ok := model.(interface{ SupportedURLs() map[string][]string })
	if !ok {
		t.Fatal("model does not implement SupportedURLs()")
	}
	got := urlsProvider.SupportedURLs()
	if got == nil || len(got) != 0 {
		t.Errorf("SupportedURLs() = %#v, want non-nil empty map", got)
	}
}
