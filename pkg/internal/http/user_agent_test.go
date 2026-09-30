package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Owner decision (2026-09-30): match the TS SDK's User-Agent behavior —
// `ai-sdk/<provider>/<version> runtime/go/<goVersion>`, appended to any
// caller-supplied User-Agent. This reverses the earlier (2026-03-29) "send
// no custom User-Agent" decision: every provider now tags its requests the
// way TS providers do, and this shared dispatch layer appends the runtime
// tag downstream of each provider's own tag.

// TestClientDoAppendsUserAgent verifies the base HTTP client always appends
// the runtime tag to whatever User-Agent the request already carries.
func TestClientDoAppendsUserAgent(t *testing.T) {
	var capturedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL})
	_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	want := providerutils.RuntimeEnvironmentUserAgent()
	if capturedUA != want {
		t.Errorf("User-Agent = %q, want %q", capturedUA, want)
	}
}

// TestClientDoAppendsToCallerSuppliedUserAgent covers a provider that has
// already set its own `ai-sdk/<name>/VERSION` tag in its default headers
// (via version.ProviderUserAgent/WithUserAgentSuffix at construction) — the
// shared dispatch layer must append the runtime tag, not replace the
// existing value. The final shape is the owner's
// `ai-sdk/<provider>/<version> runtime/go/<goVersion>`.
func TestClientDoAppendsToCallerSuppliedUserAgent(t *testing.T) {
	var capturedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	providerTag := version.ProviderUserAgent("openai")
	client := NewClient(Config{
		BaseURL: srv.URL,
		Headers: version.WithUserAgentSuffix(nil, providerTag),
	})
	_, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	want := providerTag + " " + providerutils.RuntimeEnvironmentUserAgent()
	if capturedUA != want {
		t.Errorf("User-Agent = %q, want %q", capturedUA, want)
	}
	if !strings.HasPrefix(capturedUA, "ai-sdk/openai/") {
		t.Errorf("User-Agent = %q, want ai-sdk/openai/... prefix", capturedUA)
	}
}

// TestClientDoStreamAppendsUserAgent covers the streaming dispatch path
// (DoStream), which providers use for SSE responses.
func TestClientDoStreamAppendsUserAgent(t *testing.T) {
	var capturedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL})
	resp, err := client.DoStream(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	_ = resp.Body.Close()

	want := providerutils.RuntimeEnvironmentUserAgent()
	if capturedUA != want {
		t.Errorf("User-Agent = %q, want %q", capturedUA, want)
	}
}
