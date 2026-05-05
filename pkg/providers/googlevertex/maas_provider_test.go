package googlevertex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
	if got := p.provider.Client().Do; got == nil {
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
