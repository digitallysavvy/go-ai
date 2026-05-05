package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNew_CustomHeaders_Override verifies that user-supplied Headers are sent
// on outgoing requests in addition to (and overriding) the default Authorization
// / OpenAI-* headers. This enables targeting OpenAI-compatible endpoints that
// require custom headers (e.g. GitHub Copilot, internal proxies).
func TestNew_CustomHeaders_Override(t *testing.T) {
	type captured struct {
		auth        string
		editor      string
		integration string
	}
	var got captured

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		got.editor = r.Header.Get("Editor-Version")
		got.integration = r.Header.Get("Copilot-Integration-Id")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := New(Config{
		APIKey:  "key-default",
		BaseURL: srv.URL,
		Headers: map[string]string{
			"Authorization":          "Bearer copilot-token",
			"Editor-Version":         "vscode/1.99.0",
			"Copilot-Integration-Id": "vscode-chat",
		},
	})

	// Trigger a request so the headers are emitted.
	_, err := p.client.Get(context.Background(), "/")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.auth != "Bearer copilot-token" {
		t.Errorf("Authorization = %q, want override %q", got.auth, "Bearer copilot-token")
	}
	if got.editor != "vscode/1.99.0" {
		t.Errorf("Editor-Version = %q, want %q", got.editor, "vscode/1.99.0")
	}
	if got.integration != "vscode-chat" {
		t.Errorf("Copilot-Integration-Id = %q, want %q", got.integration, "vscode-chat")
	}
}

// TestNew_HTTPClient_Override verifies that a caller-supplied *http.Client is
// used by the provider. We detect this by routing through a custom transport.
func TestNew_HTTPClient_Override(t *testing.T) {
	called := false
	custom := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: 200,
				Body:       http.NoBody,
				Header:     http.Header{},
				Request:    r,
			}, nil
		}),
	}

	p := New(Config{
		APIKey:     "k",
		BaseURL:    "http://example.invalid",
		HTTPClient: custom,
	})
	_, _ = p.client.Get(context.Background(), "/")

	if !called {
		t.Fatal("custom HTTPClient transport was not invoked")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
