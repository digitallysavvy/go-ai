package anthropic

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func authHeaderOf(t *testing.T, cfg Config) http.Header {
	t.Helper()
	var got http.Header
	cfg.HTTPClient = &http.Client{Transport: authRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Clone()
		return nil, errors.New("stop")
	})}
	p := New(cfg)
	m, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	return got
}

// TS: authToken sends `Authorization: Bearer`, the key falls back to
// ANTHROPIC_API_KEY, and giving both is an InvalidArgumentError.
func TestProviderAuthTokenAndEnvKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	if h := authHeaderOf(t, Config{AuthToken: "tok"}); h.Get("Authorization") != "Bearer tok" || h.Get("X-Api-Key") != "" {
		t.Fatalf("authToken headers = %v", h)
	}
	if h := authHeaderOf(t, Config{}); h.Get("X-Api-Key") != "env-key" {
		t.Fatalf("env key headers = %v", h)
	}
	if h := authHeaderOf(t, Config{APIKey: "explicit"}); h.Get("X-Api-Key") != "explicit" {
		t.Fatalf("explicit key headers = %v", h)
	}
	// Wrapper providers must not pick up the Anthropic key.
	if h := authHeaderOf(t, Config{UserAgentName: "minimax"}); h.Get("X-Api-Key") != "" {
		t.Fatalf("wrapper leaked key: %v", h)
	}
	defer func() {
		var ia *providererrors.InvalidArgumentError
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		} else if err, _ := r.(error); !errors.As(err, &ia) {
			t.Fatalf("panic = %v", r)
		}
	}()
	New(Config{APIKey: "a", AuthToken: "b"})
}

type authRoundTripFunc func(*http.Request) (*http.Response, error)

func (f authRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// AuthToken is a credential: it must never appear in a serialized model.
func TestSerializeRedactsAuthToken(t *testing.T) {
	m, err := New(Config{AuthToken: "secret-auth-token", BaseURL: "https://example.com"}).LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	s, ok := m.(interface {
		Serialize() provider.SerializedModel
	})
	if !ok {
		t.Fatal("model is not serializable")
	}
	data, err := json.Marshal(s.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-auth-token") || strings.Contains(strings.ToLower(string(data)), "authtoken") {
		t.Fatalf("serialized model leaks the auth token: %s", data)
	}
	if !strings.Contains(string(data), "https://example.com") {
		t.Fatalf("serialized model lost BaseURL: %s", data)
	}
}

// TS getHeaders → loadApiKey runs per request: a missing key fails the
// request (no HTTP call) with a LoadAPIKeyError, and the key is read at
// request time, not at construction.
func TestMissingAPIKeyErrorsAtRequestTime(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	called := false
	var gotKey string
	hc := &http.Client{Transport: authRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		gotKey = r.Header.Get("x-api-key")
		return nil, errors.New("stop")
	})}
	p := New(Config{HTTPClient: hc}) // construction must not fail
	m, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	gen := func() error {
		_, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		return err
	}
	err = gen()
	var le *providererrors.LoadAPIKeyError
	if _, wrapped := err.(*providererrors.LoadAPIKeyError); !wrapped || !errors.As(err, &le) || called {
		t.Fatalf("err = %v called = %v", err, called)
	}
	want := "Anthropic API key is missing. Pass it using the 'apiKey' parameter or the ANTHROPIC_API_KEY environment variable."
	if le.Message != want {
		t.Fatalf("message = %q", le.Message)
	}
	if _, err := m.DoStream(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); !errors.As(err, &le) {
		t.Fatalf("stream err = %v", err)
	}
	// Key set after construction is picked up.
	t.Setenv("ANTHROPIC_API_KEY", "late")
	_ = gen()
	if gotKey != "late" {
		t.Fatalf("key = %q", gotKey)
	}
	// Explicit auth headers keep working without an API key.
	called = false
	pm, _ := New(Config{HTTPClient: hc, Headers: map[string]string{"X-Api-Key": "hdr"}}).LanguageModel("claude-sonnet-4-5")
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, _ = pm.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if !called {
		t.Fatal("explicit header config should send the request")
	}
}
