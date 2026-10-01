package mistral

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMistralReasoningEffortSupportedModelsMatchTypeScript(t *testing.T) {
	// Mirrors ai/packages/mistral/src/mistral-chat-language-model.ts.
	// Mistral maps none → "none"; all non-default levels → "high".
	for _, modelID := range []string{ModelMistralSmallLatest, ModelMistralSmall2603, ModelMistralMedium3, ModelMistralMedium35} {
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
					if _, ok := body["stream"]; ok {
						t.Fatalf("stream = %#v, want omitted for non-streaming request", body["stream"])
					}

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

func TestMistralProviderReasoningEffortOverridesTopLevel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ModelMistralMedium3)
	level := types.ReasoningHigh

	body := model.buildRequestBody(&provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"mistral": map[string]interface{}{
				"reasoningEffort": "none",
			},
		},
	}, false)

	if got := body["reasoning_effort"]; got != "none" {
		t.Fatalf("reasoning_effort = %v, want provider option override none", got)
	}
}

func TestMistralProviderReasoningEffortValidatedLikeTypeScript(t *testing.T) {
	tests := []struct {
		name string
		opts map[string]interface{}
	}{
		{
			name: "invalid enum",
			opts: map[string]interface{}{"mistral": map[string]interface{}{"reasoningEffort": "medium"}},
		},
		{
			name: "non string",
			opts: map[string]interface{}{"mistral": map[string]interface{}{"reasoningEffort": true}},
		},
		{
			name: "non object",
			opts: map[string]interface{}{"mistral": "invalid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractMistralProviderOptions(&provider.GenerateOptions{ProviderOptions: tt.opts})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestConvertMistralUsageCachedTokenPrecedence(t *testing.T) {
	numCached := 7
	raw := json.RawMessage(`{
		"prompt_tokens": 20,
		"completion_tokens": 4,
		"total_tokens": 24,
		"num_cached_tokens": 7,
		"prompt_tokens_details": {"cached_tokens": 5},
		"prompt_token_details": {"cached_tokens": 3}
	}`)
	usage := convertMistralUsage(raw)

	if usage.InputDetails == nil || usage.InputDetails.CacheReadTokens == nil {
		t.Fatal("expected cache read token details")
	}
	if got := *usage.InputDetails.CacheReadTokens; got != int64(numCached) {
		t.Fatalf("cache read tokens: want %d, got %d", numCached, got)
	}
	if got := *usage.InputDetails.NoCacheTokens; got != 13 {
		t.Fatalf("no-cache tokens: want 13, got %d", got)
	}
	// Usage.Raw is now the full decoded JSON object (see e83d6dc), so numeric
	// values come back as float64, matching encoding/json's default decode
	// target for map[string]interface{}.
	if got := usage.Raw["num_cached_tokens"]; got != float64(numCached) {
		t.Fatalf("raw num_cached_tokens: want %d, got %v", numCached, got)
	}
	if _, ok := usage.Raw["prompt_tokens_details"]; !ok {
		t.Fatalf("raw usage should retain prompt_tokens_details verbatim, got %#v", usage.Raw)
	}
}

func TestConvertMistralUsageLegacyCachedTokenFallback(t *testing.T) {
	legacyDetailsCached := 3
	raw := json.RawMessage(`{
		"prompt_tokens": 20,
		"completion_tokens": 4,
		"total_tokens": 24,
		"prompt_token_details": {"cached_tokens": 3}
	}`)
	usage := convertMistralUsage(raw)

	if usage.InputDetails == nil || usage.InputDetails.CacheReadTokens == nil {
		t.Fatal("expected cache read token details")
	}
	if got := *usage.InputDetails.CacheReadTokens; got != int64(legacyDetailsCached) {
		t.Fatalf("cache read tokens: want %d, got %d", legacyDetailsCached, got)
	}
}

// TestConvertMistralUsageCacheReadPrecedence guards convert-mistral-usage.ts's
// exact cacheRead precedence: num_cached_tokens ??
// prompt_tokens_details.cached_tokens ?? prompt_token_details.cached_tokens ??
// 0. Mistral has no cache-write concept (cacheWrite is always undefined in
// TS), and "cache_read_input_tokens"/"cache_creation_input_tokens" are not
// Mistral usage fields at all (absent from mistralUsageSchema), so they must
// not be read as cache tokens even though they survive into raw.
func TestConvertMistralUsageCacheReadPrecedence(t *testing.T) {
	cacheRead := 11
	raw := json.RawMessage(`{
		"prompt_tokens": 30,
		"completion_tokens": 6,
		"total_tokens": 36,
		"num_cached_tokens": 11,
		"cache_read_input_tokens": 999,
		"cache_creation_input_tokens": 999,
		"prompt_tokens_details": {"cached_tokens": 5}
	}`)
	usage := convertMistralUsage(raw)

	if usage.InputDetails == nil || usage.InputDetails.CacheReadTokens == nil {
		t.Fatal("expected cache read tokens")
	}
	// num_cached_tokens takes precedence over prompt_tokens_details.cached_tokens.
	if got := *usage.InputDetails.CacheReadTokens; got != int64(cacheRead) {
		t.Fatalf("cache read tokens: want %d, got %d", cacheRead, got)
	}
	// Mistral has no cache-write concept; TS always leaves this undefined.
	if usage.InputDetails.CacheWriteTokens != nil {
		t.Fatalf("cache write tokens: want nil, got %#v", usage.InputDetails.CacheWriteTokens)
	}
	// Not real Mistral usage fields; only "raw" preserves them verbatim.
	if got := usage.Raw["cache_read_input_tokens"]; got != float64(999) {
		t.Fatalf("raw cache_read_input_tokens: want 999, got %v", got)
	}
}

// TestConvertMistralUsageOutputAlwaysText guards TS convert-mistral-usage.ts:
// outputTokens.text is always the full completion token count and
// outputTokens.reasoning is always nil/undefined — Mistral usage never
// reports a reasoning-token breakdown.
func TestConvertMistralUsageOutputAlwaysText(t *testing.T) {
	raw := json.RawMessage(`{"prompt_tokens": 10, "completion_tokens": 7, "total_tokens": 17}`)
	usage := convertMistralUsage(raw)

	if usage.OutputDetails == nil || usage.OutputDetails.TextTokens == nil || *usage.OutputDetails.TextTokens != 7 {
		t.Fatalf("OutputDetails.TextTokens = %#v, want 7", usage.OutputDetails)
	}
	if usage.OutputDetails.ReasoningTokens != nil {
		t.Fatalf("OutputDetails.ReasoningTokens = %#v, want nil", usage.OutputDetails.ReasoningTokens)
	}
}

// TestConvertMistralUsageRawIsFullDecodedObject guards e83d6dc: Usage.Raw must
// be the entire decoded usage object, including fields the typed struct above
// doesn't model (e.g. service_tier, request_count), not a hand-picked subset.
func TestConvertMistralUsageRawIsFullDecodedObject(t *testing.T) {
	raw := json.RawMessage(`{
		"prompt_tokens": 10,
		"completion_tokens": 2,
		"total_tokens": 12,
		"service_tier": "default",
		"request_count": 3
	}`)
	usage := convertMistralUsage(raw)

	if got := usage.Raw["service_tier"]; got != "default" {
		t.Fatalf("raw service_tier: want %q, got %v", "default", got)
	}
	if got := usage.Raw["request_count"]; got != float64(3) {
		t.Fatalf("raw request_count: want 3, got %v", got)
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
		if w.Type == "unsupported" && w.Feature == "reasoning" && w.Details == "This model does not support reasoning configuration." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unsupported reasoning warning, got: %+v", result.Warnings)
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

func TestMistralReasoningDefaultNoWarning(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")
	level := types.ReasoningDefault

	warnings := model.checkReasoningWarnings(&provider.GenerateOptions{Reasoning: &level})

	if len(warnings) != 0 {
		t.Errorf("expected no warnings for provider-default reasoning, got: %+v", warnings)
	}
}

// TestMistralPromptCacheKeyForwarded guards ff35434: providerOptions.mistral.promptCacheKey
// must be forwarded as prompt_cache_key on the wire.
func TestMistralPromptCacheKeyForwarded(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	body := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"mistral": map[string]interface{}{"promptCacheKey": "cache-key-1"},
		},
	}, false)

	if got := body["prompt_cache_key"]; got != "cache-key-1" {
		t.Fatalf("prompt_cache_key = %v, want cache-key-1", got)
	}
}

func TestMistralPromptCacheKeyOmittedWhenUnset(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key should be omitted, got %v", body["prompt_cache_key"])
	}
}

// TestMistralPresenceAndFrequencyPenaltyForwarded guards ec8c408: these must
// be forwarded to the wire without an "unsupported" warning.
func TestMistralPresenceAndFrequencyPenaltyForwarded(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	freq := 0.5
	presence := -0.3
	opts := &provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		FrequencyPenalty: &freq,
		PresencePenalty:  &presence,
	}
	body := model.buildRequestBody(opts, false)
	if got := body["frequency_penalty"]; got != freq {
		t.Fatalf("frequency_penalty = %v, want %v", got, freq)
	}
	if got := body["presence_penalty"]; got != presence {
		t.Fatalf("presence_penalty = %v, want %v", got, presence)
	}

	warnings := model.checkReasoningWarnings(opts)
	for _, w := range warnings {
		if w.Feature == "frequencyPenalty" || w.Feature == "presencePenalty" {
			t.Fatalf("unexpected unsupported warning for penalty option: %+v", w)
		}
	}
}

// TestMistralTopKWarning guards TS's `if (topK != null) warnings.push(...)`.
func TestMistralTopKWarning(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "mistral-large-latest")

	topK := 40
	warnings := model.checkReasoningWarnings(&provider.GenerateOptions{TopK: &topK})
	found := false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "topK" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected topK unsupported warning, got: %+v", warnings)
	}
}

// TestMistralReasoningEffortModelIDsMatchTypeScript guards f87010e: the
// reasoning-effort model gating set must match the current TS
// reasoningEffortModelIds list exactly.
func TestMistralReasoningEffortModelIDsMatchTypeScript(t *testing.T) {
	wantSupported := []string{
		"glm-5-2",
		"labs-leanstral-1-5",
		"labs-leanstral-1-5-1",
		"magistral-medium-latest",
		"magistral-small-latest",
		"mistral-medium",
		"mistral-medium-2604",
		"mistral-medium-3",
		"mistral-medium-3-5",
		"mistral-medium-3.5",
		"mistral-medium-latest",
		"mistral-small-2603",
		"mistral-small-latest",
		"mistral-vibe-cli-fast",
		"mistral-vibe-cli-latest",
		"mistral-vibe-cli-with-tools",
		"zai-glm-5-2",
	}
	wantUnsupported := []string{
		"codestral-2508",
		"codestral-latest",
		"ministral-3b-latest",
		"ministral-8b-latest",
		"ministral-14b-latest",
		"mistral-code-latest",
		"mistral-code-fim-latest",
		"mistral-large-latest",
		"mistral-large-2512",
		"voxtral-small-latest",
		"voxtral-small-2507",
	}

	for _, id := range wantSupported {
		t.Run("supported/"+id, func(t *testing.T) {
			m := NewLanguageModel(New(Config{APIKey: "k"}), id)
			if !m.supportsReasoningEffort() {
				t.Errorf("%s: want reasoning-effort support", id)
			}
		})
	}
	for _, id := range wantUnsupported {
		t.Run("unsupported/"+id, func(t *testing.T) {
			m := NewLanguageModel(New(Config{APIKey: "k"}), id)
			if m.supportsReasoningEffort() {
				t.Errorf("%s: want no reasoning-effort support", id)
			}
		})
	}
}
