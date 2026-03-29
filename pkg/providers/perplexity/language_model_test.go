package perplexity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPerplexityReasoningWarning(t *testing.T) {
	// Minimal server returning a valid OpenAI-compatible response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test","model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning for unsupported reasoning, got none")
	}
	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported-setting" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unsupported-setting warning, got: %+v", result.Warnings)
	}
}

func TestPerplexityNoWarningWhenReasoningNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test","model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	opts := &provider.GenerateOptions{}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings when Reasoning is nil, got: %+v", result.Warnings)
	}
}

// TestPerplexityCostInMetadata verifies that when the Perplexity API returns a
// nested usage.cost object, the cost fields are surfaced in
// providerMetadata["perplexity"].Cost — matching the TS SDK structure.
func TestPerplexityCostInMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Cost is a nested object under usage.cost (matches real Perplexity API).
		w.Write([]byte(`{
			"id":"test","model":"sonar",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],
			"usage":{
				"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,
				"citation_tokens":3,"num_search_queries":1,
				"cost":{
					"input_tokens_cost":0.001,
					"output_tokens_cost":0.002,
					"request_cost":0.0005,
					"total_cost":0.0035
				}
			}
		}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata is nil; expected perplexity metadata")
	}

	raw, ok := result.ProviderMetadata["perplexity"]
	if !ok {
		t.Fatalf("ProviderMetadata missing 'perplexity' key, got: %+v", result.ProviderMetadata)
	}

	meta, ok := raw.(PerplexityMetadata)
	if !ok {
		t.Fatalf("ProviderMetadata['perplexity'] type = %T, want PerplexityMetadata", raw)
	}

	// usage sub-fields
	if meta.Usage.CitationTokens == nil || *meta.Usage.CitationTokens != 3 {
		t.Errorf("Usage.CitationTokens = %v, want 3", meta.Usage.CitationTokens)
	}
	if meta.Usage.NumSearchQueries == nil || *meta.Usage.NumSearchQueries != 1 {
		t.Errorf("Usage.NumSearchQueries = %v, want 1", meta.Usage.NumSearchQueries)
	}

	// cost sub-object must be present and have correct values
	if meta.Cost == nil {
		t.Fatal("Cost is nil; expected cost object")
	}
	checkCost := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s is nil, want %v", name, want)
			return
		}
		if *got != want {
			t.Errorf("%s = %v, want %v", name, *got, want)
		}
	}
	checkCost("Cost.InputTokensCost", meta.Cost.InputTokensCost, 0.001)
	checkCost("Cost.OutputTokensCost", meta.Cost.OutputTokensCost, 0.002)
	checkCost("Cost.RequestCost", meta.Cost.RequestCost, 0.0005)
	checkCost("Cost.TotalCost", meta.Cost.TotalCost, 0.0035)
}

// TestPerplexityMetadataAlwaysPresent verifies that providerMetadata["perplexity"]
// is always set — even when no cost information is returned — matching the TS SDK
// which unconditionally sets this field.
func TestPerplexityMetadataAlwaysPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test","model":"sonar","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata should always be set for Perplexity responses")
	}
	meta, ok := result.ProviderMetadata["perplexity"].(PerplexityMetadata)
	if !ok {
		t.Fatalf("ProviderMetadata['perplexity'] type = %T, want PerplexityMetadata", result.ProviderMetadata["perplexity"])
	}
	// Cost must be nil when not in response.
	if meta.Cost != nil {
		t.Errorf("Cost should be nil when not in response, got: %+v", meta.Cost)
	}
	// Images must be nil/empty when not in response.
	if len(meta.Images) != 0 {
		t.Errorf("Images should be empty when not in response, got: %+v", meta.Images)
	}
}

// TestPerplexityImagesInMetadata verifies that the images array from the API
// response is mapped to providerMetadata["perplexity"].Images with camelCase
// field names (imageUrl, originUrl) — matching the TS SDK public shape.
func TestPerplexityImagesInMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id":"test","model":"sonar",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}],
			"images":[{"image_url":"https://img.example.com/a.jpg","origin_url":"https://example.com/a","height":100,"width":200}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
		}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	meta, ok := result.ProviderMetadata["perplexity"].(PerplexityMetadata)
	if !ok {
		t.Fatalf("expected PerplexityMetadata, got %T", result.ProviderMetadata["perplexity"])
	}
	if len(meta.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(meta.Images))
	}
	img := meta.Images[0]
	if img.ImageUrl != "https://img.example.com/a.jpg" {
		t.Errorf("ImageUrl = %q, want %q", img.ImageUrl, "https://img.example.com/a.jpg")
	}
	if img.OriginUrl != "https://example.com/a" {
		t.Errorf("OriginUrl = %q, want %q", img.OriginUrl, "https://example.com/a")
	}
	if img.Height != 100 || img.Width != 200 {
		t.Errorf("Height/Width = %d/%d, want 100/200", img.Height, img.Width)
	}
}

func TestPerplexityReasoningNotAddedToBody(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "sonar")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["reasoning_effort"]; ok {
		t.Error("Perplexity should not set reasoning_effort in body; warning is emitted instead")
	}
}
