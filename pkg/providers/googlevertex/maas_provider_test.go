package googlevertex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"golang.org/x/oauth2"
)

func TestNewMaaS_LazyInit(t *testing.T) {
	p := NewMaaS(MaaSConfig{})
	if p == nil {
		t.Fatal("expected provider")
	}
	if p.provider != nil {
		t.Fatal("expected lazy initialization")
	}
}

func TestNewMaaS_DefaultLocationAndEnvFallback(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "env-project")
	t.Setenv("GOOGLE_VERTEX_ACCESS_TOKEN", "env-token")

	p := NewMaaS(MaaSConfig{})
	model, err := p.LanguageModel(string(MaaSModelDeepSeekR1))
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if model.Provider() != "vertex.maas" {
		t.Fatalf("Provider() = %q, want vertex.maas", model.Provider())
	}
	if p.provider == nil {
		t.Fatal("expected provider to be initialized")
	}
	if p.provider.Client() == nil {
		t.Fatal("expected initialized client")
	}
}

func TestNewMaaS_CustomBaseURLStripsTrailingSlash(t *testing.T) {
	p := NewMaaS(MaaSConfig{
		BaseURL:           "https://custom-endpoint.example.com/",
		GoogleAuthOptions: &GoogleAuthOptions{TokenSource: staticTokenSource("token")},
	})
	_, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if p.provider == nil {
		t.Fatal("expected provider")
	}
}

func TestNewMaaS_CustomBaseURLDoesNotRequireProject(t *testing.T) {
	p := NewMaaS(MaaSConfig{
		BaseURL:           "https://custom-endpoint.example.com",
		GoogleAuthOptions: &GoogleAuthOptions{TokenSource: staticTokenSource("token")},
	})
	if _, err := p.LanguageModel("test-model"); err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
}

// TestNewMaaS_RejectsLocationThatWouldRewriteHost ports TS
// google-vertex-location-validation.test.ts for the MaaS provider (TS
// #21842): location is interpolated directly into the request host, so a
// value that isn't a single DNS label must be rejected before init.
func TestNewMaaS_RejectsLocationThatWouldRewriteHost(t *testing.T) {
	for _, location := range []string{"user@internal:8080/#", "evil.example.com/#", "us central 1"} {
		t.Run(location, func(t *testing.T) {
			p := NewMaaS(MaaSConfig{
				Project:           "test-project",
				Location:          location,
				GoogleAuthOptions: &GoogleAuthOptions{TokenSource: staticTokenSource("token")},
			})
			_, err := p.LanguageModel("test-model")
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("LanguageModel(location=%q) error = %v, want InvalidArgumentError", location, err)
			}
			if invalid.Field != "location" {
				t.Fatalf("Field = %q, want location", invalid.Field)
			}
		})
	}
}

// TestNewMaaS_DoesNotValidateUnusedLocationWithCustomBaseURL ports TS "does
// not validate an unused location with a custom endpoint".
func TestNewMaaS_DoesNotValidateUnusedLocationWithCustomBaseURL(t *testing.T) {
	p := NewMaaS(MaaSConfig{
		Location:          "user@internal:8080/#",
		BaseURL:           "https://custom-endpoint.example.com",
		GoogleAuthOptions: &GoogleAuthOptions{TokenSource: staticTokenSource("token")},
	})
	if _, err := p.LanguageModel("test-model"); err != nil {
		t.Fatalf("LanguageModel error = %v, want success (location unused with explicit BaseURL)", err)
	}
}

func TestMaaSBaseURLUsesMultiRegionHosts(t *testing.T) {
	tests := []struct {
		location string
		want     string
	}{
		{
			location: "global",
			want:     "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/endpoints/openapi",
		},
		{
			location: "eu",
			want:     "https://aiplatform.eu.rep.googleapis.com/v1/projects/test-project/locations/eu/endpoints/openapi",
		},
		{
			location: "us",
			want:     "https://aiplatform.us.rep.googleapis.com/v1/projects/test-project/locations/us/endpoints/openapi",
		},
		{
			location: "us-central1",
			want:     "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/endpoints/openapi",
		},
	}
	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			if got := maasBaseURL("test-project", tt.location); got != tt.want {
				t.Fatalf("maasBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewMaaS_ImageModel_RequiresModelID(t *testing.T) {
	p := NewMaaS(MaaSConfig{
		BaseURL:           "https://custom-endpoint.example.com",
		GoogleAuthOptions: &GoogleAuthOptions{TokenSource: staticTokenSource("token")},
	})
	if _, err := p.ImageModel(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestVertexMaaS_DefaultProvider(t *testing.T) {
	if VertexMaaS == nil {
		t.Fatal("expected default provider")
	}
	if VertexMaaS.Name() != "vertex.maas" {
		t.Fatalf("Name() = %q", VertexMaaS.Name())
	}
}

// TestNewMaaS_UserAgentTaggedOpenAICompatibleNotGoogleVertex mirrors TS
// google-vertex-maas-provider.ts, which builds on @ai-sdk/openai-compatible's
// createOpenAICompatible (not @ai-sdk/google-vertex), so its requests carry
// openai-compatible's own "ai-sdk/openai-compatible/VERSION" tag, never
// "ai-sdk/google-vertex".
func TestNewMaaS_UserAgentTaggedOpenAICompatibleNotGoogleVertex(t *testing.T) {
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		Project: "test-project",
		BaseURL: server.URL,
		GoogleAuthOptions: &GoogleAuthOptions{
			TokenSource: staticTokenSource("dynamic-token"),
		},
	})
	model, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if !strings.HasPrefix(gotUserAgent, "ai-sdk/openai-compatible/") {
		t.Fatalf("User-Agent = %q, want ai-sdk/openai-compatible/... prefix", gotUserAgent)
	}
	if strings.Contains(gotUserAgent, "ai-sdk/google-vertex/") {
		t.Fatalf("User-Agent = %q, must not carry the ai-sdk/google-vertex tag", gotUserAgent)
	}
}

func TestNewMaaS_AuthTokenInjectedPerRequest(t *testing.T) {
	var authHeader string
	var customHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		customHeader = r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		Project: "test-project",
		BaseURL: server.URL,
		Headers: StaticHeaders(map[string]string{"X-Custom": "header"}),
		GoogleAuthOptions: &GoogleAuthOptions{
			TokenSource: staticTokenSource("dynamic-token"),
		},
	})
	model, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if authHeader != "Bearer dynamic-token" {
		t.Fatalf("Authorization = %q, want Bearer dynamic-token", authHeader)
	}
	if customHeader != "header" {
		t.Fatalf("X-Custom = %q, want header", customHeader)
	}
}

func TestNewMaaS_DynamicHeadersInjectedPerRequest(t *testing.T) {
	var dynamicHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dynamicHeader = r.Header.Get("X-Dynamic")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		BaseURL: server.URL,
		Headers: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"X-Dynamic": "resolved"}, nil
		},
		GoogleAuthOptions: &GoogleAuthOptions{
			TokenSource: staticTokenSource("dynamic-token"),
		},
	})
	model, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if dynamicHeader != "resolved" {
		t.Fatalf("X-Dynamic = %q, want resolved", dynamicHeader)
	}
}

func TestNewMaaS_ConfigAuthTokenOverride(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		BaseURL: server.URL,
		AuthToken: func(context.Context) (string, error) {
			return "override-token", nil
		},
	})
	model, err := p.LanguageModel("xai/grok-4.1-fast-reasoning")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if authHeader != "Bearer override-token" {
		t.Fatalf("Authorization = %q, want Bearer override-token", authHeader)
	}
}

func TestMaaSModelIDs_NonEmpty(t *testing.T) {
	ids := []GoogleVertexMaasModelID{
		MaaSModelDeepSeekR1,
		MaaSModelDeepSeekV31,
		MaaSModelDeepSeekV32,
		MaaSModelGPTOSS120B,
		MaaSModelGPTOSS20B,
		MaaSModelLlama4Maverick17B,
		MaaSModelLlama4Scout17B,
		MaaSModelMiniMaxM2,
		MaaSModelQwen3Coder480B,
		MaaSModelQwen3Next80B,
		MaaSModelQwen3Next80BThink,
		MaaSModelKimiK2Thinking,
		MaaSModelGrok420Reasoning,
		MaaSModelGrok420NonReasoning,
		MaaSModelGrok41FastReasoning,
		MaaSModelGrok41FastNonReasoning,
	}
	for _, id := range ids {
		if id == "" {
			t.Fatal("expected non-empty model ID")
		}
	}
}

func TestDefaultMaasAuthToken_Env(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_ACCESS_TOKEN", "env-token")
	token, err := defaultMaasAuthToken(context.Background())
	if err != nil {
		t.Fatalf("defaultMaasAuthToken error = %v", err)
	}
	if token != "env-token" {
		t.Fatalf("token = %q, want env-token", token)
	}
}

func TestDefaultMaasAuthToken_Missing(t *testing.T) {
	_ = os.Unsetenv("GOOGLE_VERTEX_ACCESS_TOKEN")
	_, err := defaultMaasAuthToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestDefaultMaasAuthToken_CachesAcrossCalls is a regression test: the ADC
// path of defaultMaasAuthToken previously fetched a brand-new
// google.DefaultTokenSource(ctx, scope).Token() on every single call instead
// of caching the token near its expiry, unlike the main googlevertex
// provider's own cachedTokenSource. Each GCE access token is normally valid
// for ~1 hour, so re-fetching per call pays needless latency/I/O and risks
// tripping the metadata server's documented rate limits under a busy
// workload. Uses a fake GCE metadata server (GCE_METADATA_HOST) returning a
// 3600s-valid token; 3 calls sharing that still-valid token must hit the
// metadata server's token endpoint exactly once.
//
// Runs in a freshly spawned subprocess:
// cloud.google.com/go/compute/metadata's OnGCE() memoizes its result for the
// life of the process (a package-level sync.Once), so if any earlier test in
// this binary already called google.DefaultTokenSource with no ADC
// configured (e.g. TestDefaultMaasAuthToken_Missing), OnGCE() would already
// be cached as false and our fake GCE_METADATA_HOST would never be
// consulted. A child process guarantees OnGCE() is resolved fresh, against
// the env this test sets up.
func TestDefaultMaasAuthToken_CachesAcrossCalls(t *testing.T) {
	if os.Getenv("GOOGLEVERTEX_MAAS_CACHE_TEST_CHILD") == "1" {
		runDefaultMaasAuthTokenCacheChild()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestDefaultMaasAuthToken_CachesAcrossCalls$", "-test.v")
	cmd.Env = append(os.Environ(), "GOOGLEVERTEX_MAAS_CACHE_TEST_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\noutput:\n%s", err, out)
	}
}

func runDefaultMaasAuthTokenCacheChild() {
	var tokenHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/computeMetadata/v1/project/project-id", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "test-project")
	})
	mux.HandleFunc("/computeMetadata/v1/instance/service-accounts/default/token", func(w http.ResponseWriter, r *http.Request) {
		hits := atomic.AddInt32(&tokenHits, 1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": fmt.Sprintf("token-%d", hits), "expires_in": 3600, "token_type": "Bearer",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hostOnly, _ := url.Parse(srv.URL)

	_ = os.Setenv("GCE_METADATA_HOST", hostOnly.Host)
	_ = os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")
	_ = os.Unsetenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := defaultMaasAuthToken(ctx); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "call %d failed: %v\n", i, err)
			os.Exit(1)
		}
	}
	if got := atomic.LoadInt32(&tokenHits); got != 1 {
		_, _ = fmt.Fprintf(os.Stderr, "hit metadata token endpoint %d times for 3 requests sharing a still-valid (3600s) token; expected 1 (cached), got %d\n", got, got)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestNewMaaS_ProviderErrorsUseVertexProviderName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		BaseURL: server.URL,
		GoogleAuthOptions: &GoogleAuthOptions{
			TokenSource: staticTokenSource("dynamic-token"),
		},
	})
	model, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{}})
	if err == nil {
		t.Fatal("expected error")
	}

	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Provider != "vertex.maas" {
		t.Fatalf("provider = %q, want %q", providerErr.Provider, "vertex.maas")
	}
}

func staticTokenSource(token string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
}

// TestNewMaaS_Llama4DefaultMaxTokens ports TS
// transformGoogleVertexMaasRequestBody (google-vertex-maas-provider.ts):
// llama-4 MaaS models get max_tokens=8192 injected when the caller doesn't
// set one, since they otherwise silently truncate output.
func TestNewMaaS_Llama4DefaultMaxTokens(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"meta/llama-4-maverick-17b-128e-instruct-maas","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		Project: "test-project",
		BaseURL: server.URL,
		GoogleAuthOptions: &GoogleAuthOptions{
			TokenSource: staticTokenSource("dynamic-token"),
		},
	})
	model, err := p.LanguageModel("meta/llama-4-maverick-17b-128e-instruct-maas")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if got, ok := gotBody["max_tokens"].(float64); !ok || got != 8192 {
		t.Fatalf("max_tokens = %v, want 8192", gotBody["max_tokens"])
	}

	// An explicit MaxTokens is never overridden.
	gotBody = nil
	explicit := 100
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		MaxTokens: &explicit,
	}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if got, ok := gotBody["max_tokens"].(float64); !ok || got != 100 {
		t.Fatalf("max_tokens = %v, want 100 (explicit)", gotBody["max_tokens"])
	}

	// A non-llama-4 model is left alone.
	gotBody = nil
	other, err := p.LanguageModel("some-other-model")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	if _, err := other.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if _, ok := gotBody["max_tokens"]; ok {
		t.Fatalf("expected no max_tokens for non-llama-4 model, got %v", gotBody["max_tokens"])
	}
}
