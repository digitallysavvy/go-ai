package anthropicaws

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type captureTransport struct {
	req *http.Request
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.req = req.Clone(req.Context())
	return &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"type":"message",
			"id":"msg_123",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"hi"}],
			"usage":{"input_tokens":1,"output_tokens":1}
		}`)),
	}, nil
}

func TestAnthropicAWSAPIKeyHeaders(t *testing.T) {
	transport := &captureTransport{}
	p, err := New(Config{
		Region:      "us-west-2",
		WorkspaceID: "wrkspc_test",
		APIKey:      "sk-aws-platform-key",
		HTTPClient:  &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, err := p.LanguageModel("claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if got := transport.req.URL.String(); got != "https://aws-external-anthropic.us-west-2.api.aws/v1/messages" {
		t.Fatalf("url = %q", got)
	}
	if got := transport.req.Header.Get("x-api-key"); got != "sk-aws-platform-key" {
		t.Fatalf("x-api-key = %q", got)
	}
	if got := transport.req.Header.Get("anthropic-workspace-id"); got != "wrkspc_test" {
		t.Fatalf("workspace header = %q", got)
	}
	if got := transport.req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty for API-key auth", got)
	}
}

func TestAnthropicAWSSigV4HeadersAndCredentialProvider(t *testing.T) {
	transport := &captureTransport{}
	called := false
	p, err := New(Config{
		Region:      "us-west-2",
		WorkspaceID: "wrkspc_test",
		HTTPClient:  &http.Client{Transport: transport},
		CredentialProvider: func(context.Context) (Credentials, error) {
			called = true
			return Credentials{AccessKeyID: "akid", SecretAccessKey: "secret", SessionToken: "session"}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model, _ := p.LanguageModel("claude-sonnet-4-6")
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if !called {
		t.Fatal("credential provider was not called")
	}
	if got := transport.req.Header.Get("Authorization"); !strings.Contains(got, "AWS4-HMAC-SHA256 Credential=akid/") {
		t.Fatalf("Authorization = %q", got)
	}
	if got := transport.req.Header.Get("X-Amz-Security-Token"); got != "session" {
		t.Fatalf("X-Amz-Security-Token = %q", got)
	}
}
