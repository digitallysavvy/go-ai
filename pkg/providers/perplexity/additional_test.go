package perplexity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestPerplexityProvider_FactoriesAndUnsupported mirrors the provider-level
// factory smoke test: language/embedding models are supported, everything
// else returns an error.
func TestPerplexityProvider_FactoriesAndUnsupported(t *testing.T) {
	t.Parallel()

	p := CreatePerplexity(Config{APIKey: "k", BaseURL: "https://example.test"})
	if p.Name() != "perplexity" {
		t.Fatalf("Name() = %q, want perplexity", p.Name())
	}
	if p.Client() == nil {
		t.Fatal("Client() returned nil")
	}

	modelAny, err := p.LanguageModel("")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}
	if modelAny.ModelID() != DefaultModelID {
		t.Fatalf("default model ID = %q", modelAny.ModelID())
	}

	embModel, err := p.EmbeddingModel("x")
	if err != nil {
		t.Fatalf("EmbeddingModel() error = %v, want supported", err)
	}
	if embModel.ModelID() != "x" {
		t.Fatalf("EmbeddingModel().ModelID() = %q, want x", embModel.ModelID())
	}
	if _, err := p.ImageModel("x"); err == nil {
		t.Fatal("ImageModel expected unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
}

func TestPerplexityModelIDConstantsMatchTypeScript(t *testing.T) {
	want := map[string]PerplexityLanguageModelID{
		"sonar-deep-research": ModelSonarDeepResearch,
		"sonar-reasoning-pro": ModelSonarReasoningPro,
		"sonar-reasoning":     ModelSonarReasoning,
		"sonar-pro":           ModelSonarPro,
		"sonar":               ModelSonar,
	}
	for wantString, got := range want {
		if string(got) != wantString {
			t.Fatalf("model constant = %q, want %q", got, wantString)
		}
	}
}

func TestPerplexityAgentPresets(t *testing.T) {
	for _, preset := range []string{"fast", "low", "medium", "high", "xhigh"} {
		if !isPerplexityAgentPreset(preset) {
			t.Fatalf("isPerplexityAgentPreset(%q) = false, want true", preset)
		}
		model, presetOut := getModelSelection(preset)
		if model != "" || presetOut != preset {
			t.Fatalf("getModelSelection(%q) = (%q, %q), want (\"\", %q)", preset, model, presetOut, preset)
		}
	}

	// Legacy Sonar model IDs and direct provider/model IDs are NOT presets --
	// they are sent as the "model" field, matching TS "does not map legacy
	// Sonar model IDs to Agent API presets".
	for _, modelID := range []string{"sonar-pro", "sonar", "openai/gpt-5.1", "perplexity/sonar"} {
		if isPerplexityAgentPreset(modelID) {
			t.Fatalf("isPerplexityAgentPreset(%q) = true, want false", modelID)
		}
		model, preset := getModelSelection(modelID)
		if model != modelID || preset != "" {
			t.Fatalf("getModelSelection(%q) = (%q, %q), want (%q, \"\")", modelID, model, preset, modelID)
		}
	}
}

func TestPerplexityProviderLoadsAPIKeyFromEnvironmentAndUsesAgentEndpoint(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "env-key")

	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/agent" {
			t.Fatalf("request path = %q, want /v1/agent", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","created_at":1770768220,"model":"sonar","object":"response","status":"completed","output":[]}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL + "/"})
	model := NewLanguageModel(p, "sonar")
	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if auth != "Bearer env-key" {
		t.Fatalf("Authorization = %q, want Bearer env-key", auth)
	}
}

func TestPerplexityProviderSendsIntegrationAttributionHeader(t *testing.T) {
	var integration string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integration = r.Header.Get("X-Pplx-Integration")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","created_at":1770768220,"model":"sonar","object":"response","status":"completed","output":[]}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL + "/"})
	model := NewLanguageModel(p, "sonar")
	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if integration != "vercel-ai-sdk" {
		t.Fatalf("X-Pplx-Integration = %q, want vercel-ai-sdk", integration)
	}
}

func TestPerplexityProviderIntegrationAttributionHeaderCanBeOverridden(t *testing.T) {
	var integration string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integration = r.Header.Get("X-Pplx-Integration")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","created_at":1770768220,"model":"sonar","object":"response","status":"completed","output":[]}`))
	}))
	defer srv.Close()

	p := New(Config{
		APIKey:  "test-key",
		BaseURL: srv.URL + "/",
		Headers: map[string]string{"X-Pplx-Integration": "custom"},
	})
	model := NewLanguageModel(p, "sonar")
	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if integration != "custom" {
		t.Fatalf("X-Pplx-Integration = %q, want custom", integration)
	}
}

func TestPerplexityLanguageModel_Metadata(t *testing.T) {
	t.Parallel()

	m := NewLanguageModel(New(Config{APIKey: "k"}), "sonar")
	if m.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.Provider() != "perplexity" || m.ModelID() != "sonar" {
		t.Fatalf("provider/model mismatch: %s/%s", m.Provider(), m.ModelID())
	}
	if !m.SupportsTools() || !m.SupportsStructuredOutput() || !m.SupportsImageInput() {
		t.Fatal("Agent API model should support tools, structured output, and image input")
	}
}
