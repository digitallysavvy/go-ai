package anthropic

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Ports the "baseURL configuration" describe block of anthropic-provider.test.ts
// (ai@7.0.113). TS distinguishes an unset baseURL (undefined) from an
// explicitly empty one (""); Go's string zero value can't, so by convention
// "" means "unset" here (see providerutils.ValidateBaseURL and gotcha 679c52a
// in HANDOFF.md) while an explicitly non-empty, whitespace-only value is
// still rejected.

func withEnv(t *testing.T, key, value string) {
	t.Helper()
	original, had := os.LookupEnv(key)
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, original)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestNormalizeBaseURLDefault(t *testing.T) {
	withEnv(t, "ANTHROPIC_BASE_URL", "")
	got, err := NormalizeBaseURL("")
	if err != nil {
		t.Fatalf("NormalizeBaseURL: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty (caller falls back to DefaultBaseURL)", got)
	}
}

// TestNewUsesDefaultBaseURL ports "uses the default Anthropic base URL when
// not provided".
func TestNewUsesDefaultBaseURL(t *testing.T) {
	withEnv(t, "ANTHROPIC_BASE_URL", "")
	var gotURL string
	prov := New(Config{
		APIKey: "test-api-key",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotURL = r.URL.String()
			return jsonOKResponse(), nil
		})},
	})
	model, err := prov.LanguageModel("claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotURL != "https://api.anthropic.com/v1/messages" {
		t.Errorf("url = %q, want https://api.anthropic.com/v1/messages", gotURL)
	}
}

// TestNewUsesAnthropicBaseURLEnv ports "uses ANTHROPIC_BASE_URL when set".
func TestNewUsesAnthropicBaseURLEnv(t *testing.T) {
	withEnv(t, "ANTHROPIC_BASE_URL", "https://proxy.anthropic.example/v1/")
	var gotURL string
	prov := New(Config{
		APIKey: "test-api-key",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotURL = r.URL.String()
			return jsonOKResponse(), nil
		})},
	})
	model, err := prov.LanguageModel("claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if gotURL != "https://proxy.anthropic.example/v1/messages" {
		t.Errorf("url = %q, want https://proxy.anthropic.example/v1/messages", gotURL)
	}
}

// TestNewNormalizesBareHostFromEnv ports "normalizes a bare Anthropic API URL
// from ANTHROPIC_BASE_URL".
func TestNewNormalizesBareHostFromEnv(t *testing.T) {
	withEnv(t, "ANTHROPIC_BASE_URL", "https://api.anthropic.com/")
	got, err := NormalizeBaseURL("")
	if err != nil {
		t.Fatalf("NormalizeBaseURL: %v", err)
	}
	if got != DefaultBaseURL {
		t.Errorf("got %q, want %q", got, DefaultBaseURL)
	}
}

// TestNewNormalizesBareHostFromOption ports "normalizes a bare Anthropic API
// URL from the baseURL option".
func TestNewNormalizesBareHostFromOption(t *testing.T) {
	got, err := NormalizeBaseURL("https://api.anthropic.com/")
	if err != nil {
		t.Fatalf("NormalizeBaseURL: %v", err)
	}
	if got != DefaultBaseURL {
		t.Errorf("got %q, want %q", got, DefaultBaseURL)
	}
}

// TestNewPrefersOptionOverEnv ports "prefers the baseURL option over
// ANTHROPIC_BASE_URL".
func TestNewPrefersOptionOverEnv(t *testing.T) {
	withEnv(t, "ANTHROPIC_BASE_URL", "https://env.anthropic.example/v1")
	got, err := NormalizeBaseURL("https://option.anthropic.example/v1/")
	if err != nil {
		t.Fatalf("NormalizeBaseURL: %v", err)
	}
	if got != "https://option.anthropic.example/v1" {
		t.Errorf("got %q, want https://option.anthropic.example/v1", got)
	}
}

// TestNewRejectsWhitespaceOnlyBaseURL ports "rejects an empty baseURL option
// during provider creation" (adapted: Go's zero-value "" means "unset", so
// the Go-equivalent invalid input is a whitespace-only string; see gotcha
// cd12954 / providerutils.ValidateBaseURL).
func TestNewRejectsWhitespaceOnlyBaseURL(t *testing.T) {
	_, err := NormalizeBaseURL("   ")
	if err == nil {
		t.Fatal("expected an error for a whitespace-only baseURL")
	}
	if err.Error() != "baseURL must be a non-empty string." {
		t.Errorf("err = %q, want %q", err.Error(), "baseURL must be a non-empty string.")
	}
	if err != providerutils.ErrEmptyBaseURL {
		t.Errorf("err = %v, want providerutils.ErrEmptyBaseURL", err)
	}
}

// TestNewPanicsOnWhitespaceOnlyBaseURL verifies New() panics with the
// validation error rather than silently accepting a whitespace-only BaseURL
// (HANDOFF.md gotcha: "New panics on a whitespace-only base URL").
func TestNewPanicsOnWhitespaceOnlyBaseURL(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New() to panic on a whitespace-only BaseURL")
		}
		err, ok := r.(error)
		if !ok || err.Error() != "baseURL must be a non-empty string." {
			t.Errorf("recovered = %v, want the ValidateBaseURL error", r)
		}
	}()
	New(Config{APIKey: "test-key", BaseURL: "   "})
}

// TestNewAllowsEmptyBaseURL verifies the Go zero value ("") is treated as
// "unset" and falls back to the default, unlike TS's explicit "" rejection —
// Go strings cannot distinguish "not provided" from "" the way TS
// distinguishes undefined from "".
func TestNewAllowsEmptyBaseURL(t *testing.T) {
	withEnv(t, "ANTHROPIC_BASE_URL", "")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New() should not panic for an empty BaseURL, got %v", r)
		}
	}()
	prov := New(Config{APIKey: "test-key", BaseURL: ""})
	if prov == nil {
		t.Fatal("New() returned nil")
	}
}

// --- test helpers -----------------------------------------------------------

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonOKResponse() *http.Response {
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-haiku-20240307","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
