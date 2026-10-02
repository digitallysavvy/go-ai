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

// Owner decision (2026-09-30, updated for TS #21344 on 2026-10-01): match
// the TS SDK's User-Agent behavior — `ai-sdk-<provider>/<version>
// go/<goVersion>`, appended to any caller-supplied User-Agent. This
// reverses the earlier (2026-03-29) "send no custom User-Agent" decision:
// every provider now tags its requests the way TS providers do, and this
// shared dispatch layer appends the runtime tag downstream of each
// provider's own tag. TS #21344 ("use standards-compliant User-Agent
// header") fixed both tags to carry exactly one "/" each, since an RFC
// 9110 product identifier allows only one: the provider tag's separator
// changed from "/" to "-" (`ai-sdk/<provider>` -> `ai-sdk-<provider>`),
// and the runtime tag dropped its "runtime/" prefix (`runtime/go/<ver>` ->
// `go/<ver>`).

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
// already set its own `ai-sdk-<name>/VERSION` tag in its default headers
// (via version.ProviderUserAgent/WithUserAgentSuffix at construction) — the
// shared dispatch layer must append the runtime tag, not replace the
// existing value. The final shape is the owner's
// `ai-sdk-<provider>/<version> go/<goVersion>`.
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
	if !strings.HasPrefix(capturedUA, "ai-sdk-openai/") {
		t.Errorf("User-Agent = %q, want ai-sdk-openai/... prefix", capturedUA)
	}
}

// TestClientDoTwiceWithSameHeaderMapNoDuplicateRuntimeTag covers the
// scenario called out in the owner's 2026-09-30 decision: a caller building
// one Request.Headers map and reusing it across multiple Do() calls (or a
// provider retrying the same request) must not accumulate additional
// runtime tags, since applyUserAgentSuffix mutates only the per-request
// http.Header built from that map, never the caller's map itself.
func TestClientDoTwiceWithSameHeaderMapNoDuplicateRuntimeTag(t *testing.T) {
	var capturedUAs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUAs = append(capturedUAs, r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL})
	sharedHeaders := map[string]string{"user-agent": version.ProviderUserAgent("openai")}
	req := Request{Method: http.MethodGet, Path: "/", Headers: sharedHeaders}

	if _, err := client.Do(context.Background(), req); err != nil {
		t.Fatalf("first Do() error = %v", err)
	}
	if _, err := client.Do(context.Background(), req); err != nil {
		t.Fatalf("second Do() error = %v", err)
	}

	if len(capturedUAs) != 2 {
		t.Fatalf("got %d requests, want 2", len(capturedUAs))
	}
	if capturedUAs[0] != capturedUAs[1] {
		t.Fatalf("User-Agent differs across calls: %q vs %q", capturedUAs[0], capturedUAs[1])
	}
	runtimeTag := providerutils.RuntimeEnvironmentUserAgent()
	if n := strings.Count(capturedUAs[1], runtimeTag); n != 1 {
		t.Fatalf("User-Agent = %q, want exactly one %q, got %d", capturedUAs[1], runtimeTag, n)
	}
	// The shared map passed as Request.Headers must not be mutated by Do().
	if sharedHeaders["user-agent"] != version.ProviderUserAgent("openai") {
		t.Fatalf("caller's Headers map was mutated: %q", sharedHeaders["user-agent"])
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
