package anthropicaws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sigv4check"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const okMessage = `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// SigV4 for Claude Platform on AWS: service aws-external-anthropic, scoped to
// the configured region, workspace header sent, no x-api-key.
func TestSigV4AcceptedAndHeaders(t *testing.T) {
	var mu sync.Mutex
	var reason string
	var apiKey, workspace, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reason = sigv4check.Verify(r, body, "secret-key", "eu-west-1", "aws-external-anthropic")
		apiKey, workspace, version = r.Header.Get("x-api-key"), r.Header.Get("anthropic-workspace-id"), r.Header.Get("anthropic-version")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okMessage)
	}))
	defer srv.Close()

	p, err := New(Config{Region: "eu-west-1", WorkspaceID: "wrkspc_1", BaseURL: srv.URL + "/v1", AccessKeyID: "AKID", SecretAccessKey: "secret-key", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if reason != "" {
		t.Fatalf("signature rejected: %s", reason)
	}
	if apiKey != "" || workspace != "wrkspc_1" || version != "2023-06-01" {
		t.Fatalf("headers: x-api-key=%q workspace=%q version=%q", apiKey, workspace, version)
	}
}

// API-key auth sends x-api-key and no Authorization; the key is never part of
// a SigV4 request.
func TestAPIKeyAuthNeverSigns(t *testing.T) {
	var gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		gotKey, gotAuth = r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okMessage)
	}))
	defer srv.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKID")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "w", APIKey: "aws-key", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "aws-key" || gotAuth != "" {
		t.Fatalf("x-api-key=%q Authorization=%q", gotKey, gotAuth)
	}
}

// Missing credentials fail the request with the TS guidance text instead of
// sending an unsigned request.
func TestMissingCredentialsFailBeforeSending(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("ANTHROPIC_AWS_API_KEY", "")
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "w", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "AWS SigV4 authentication requires AWS credentials") || hit {
		t.Fatalf("err = %v, request sent = %v", err, hit)
	}
}

// TS loads the region at request time on the SigV4 path and fails when it is
// missing, even with a custom baseURL; nothing is sent.
func TestSigV4MissingRegionFailsBeforeSending(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	p, err := New(Config{WorkspaceID: "wrkspc_1", BaseURL: srv.URL + "/v1", AccessKeyID: "AKID", SecretAccessKey: "secret", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "region setting is missing") {
		t.Fatalf("err = %v, want the missing region error", err)
	}
	if called {
		t.Fatal("a request without a region must not be sent")
	}
}

// The API key is read at request time: ANTHROPIC_AWS_API_KEY set after the
// provider was created is used, and no SigV4 credentials are needed.
func TestAPIKeyFromEnvReadAtRequestTime(t *testing.T) {
	var gotKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		gotKey, gotAuth = r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okMessage)
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_AWS_API_KEY", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "w", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_AWS_API_KEY", "late-key")
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "late-key" || gotAuth != "" {
		t.Fatalf("x-api-key=%q Authorization=%q", gotKey, gotAuth)
	}
}
