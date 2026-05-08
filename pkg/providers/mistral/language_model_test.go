package mistral

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMistralSmallReasoningSupported(t *testing.T) {
	// Both mistral-small-latest and mistral-small-2603 support reasoning_effort.
	// Mistral maps none → "none"; all non-default levels → "high".
	for _, modelID := range []string{ModelMistralSmallLatest, ModelMistralSmall2603, ModelMistralMedium35} {
		t.Run(modelID, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, modelID)

			tests := []struct {
				level  types.ReasoningLevel
				want   string
				hasKey bool
			}{
				{types.ReasoningNone, "none", true},
				{types.ReasoningMinimal, "high", true},
				{types.ReasoningLow, "high", true},
				{types.ReasoningMedium, "high", true},
				{types.ReasoningHigh, "high", true},
				{types.ReasoningXHigh, "high", true},
				{types.ReasoningDefault, "", false},
			}

			for _, tt := range tests {
				t.Run(string(tt.level), func(t *testing.T) {
					level := tt.level
					opts := &provider.GenerateOptions{Reasoning: &level}
					body := model.buildRequestBody(opts, false)

					val, hasKey := body["reasoning_effort"]
					if hasKey != tt.hasKey {
						t.Fatalf("reasoning_effort presence: want %v, got %v", tt.hasKey, hasKey)
					}
					if tt.hasKey && val != tt.want {
						t.Errorf("reasoning_effort: want %q, got %v", tt.want, val)
					}
				})
			}
		})
	}
}

func TestConvertMistralUsageCachedTokenPrecedence(t *testing.T) {
	numCached := 7
	promptDetailsCached := 5
	legacyDetailsCached := 3
	usage := convertMistralUsage(mistralUsage{
		PromptTokens:     20,
		CompletionTokens: 4,
		TotalTokens:      24,
		NumCachedTokens:  &numCached,
		PromptTokensDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
			AudioTokens  *int `json:"audio_tokens,omitempty"`
			TextTokens   *int `json:"text_tokens,omitempty"`
			ImageTokens  *int `json:"image_tokens,omitempty"`
		}{CachedTokens: &promptDetailsCached},
		PromptTokenDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
		}{CachedTokens: &legacyDetailsCached},
	})

	if usage.InputDetails == nil || usage.InputDetails.CacheReadTokens == nil {
		t.Fatal("expected cache read token details")
	}
	if got := *usage.InputDetails.CacheReadTokens; got != int64(numCached) {
		t.Fatalf("cache read tokens: want %d, got %d", numCached, got)
	}
	if got := *usage.InputDetails.NoCacheTokens; got != 13 {
		t.Fatalf("no-cache tokens: want 13, got %d", got)
	}
	if got := usage.Raw["num_cached_tokens"]; got != numCached {
		t.Fatalf("raw num_cached_tokens: want %d, got %v", numCached, got)
	}
}

func TestConvertMistralUsageLegacyCachedTokenFallback(t *testing.T) {
	legacyDetailsCached := 3
	usage := convertMistralUsage(mistralUsage{
		PromptTokens:     20,
		CompletionTokens: 4,
		TotalTokens:      24,
		PromptTokenDetails: &struct {
			CachedTokens *int `json:"cached_tokens,omitempty"`
		}{CachedTokens: &legacyDetailsCached},
	})

	if usage.InputDetails == nil || usage.InputDetails.CacheReadTokens == nil {
		t.Fatal("expected cache read token details")
	}
	if got := *usage.InputDetails.CacheReadTokens; got != int64(legacyDetailsCached) {
		t.Fatalf("cache read tokens: want %d, got %d", legacyDetailsCached, got)
	}
}

func TestMistralNonSmallReasoningOmittedFromBody(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["reasoning_effort"]; ok {
		t.Error("non-small model should not set reasoning_effort in body; warning is emitted instead")
	}
}

func TestMistralNonReasoningModelWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	level := types.ReasoningMedium
	opts := &provider.GenerateOptions{Reasoning: &level}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning for unsupported reasoning model, got none")
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

func TestMistralSmallNoWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-small-latest")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("mistral-small-latest should not warn for reasoning, got: %+v", result.Warnings)
	}
}

func TestMistralSmall2603NoWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-small-2603")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("mistral-small-2603 should not warn for reasoning, got: %+v", result.Warnings)
	}
}

func TestMistralReasoningNilNoWarning(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	opts := &provider.GenerateOptions{}
	warnings := model.checkReasoningWarnings(opts)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings when Reasoning is nil, got: %+v", warnings)
	}
}
