package alibaba

import (
	"encoding/json"
	"testing"
)

func TestNewProvider(t *testing.T) {
	cfg := Config{
		APIKey: "test-api-key",
	}

	prov := New(cfg)

	if prov == nil {
		t.Fatal("Expected provider to be created")
	}

	if prov.Name() != "alibaba" {
		t.Errorf("Expected provider name 'alibaba', got '%s'", prov.Name())
	}
}

func TestLanguageModelValidation(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)

	validModels := []string{
		"qwen-plus",
		"qwen-turbo",
		"qwen-max",
		"qwen3.7-max",
		"qwen-qwq-32b-preview",
		"qwen-vl-max",
		"custom-model-id",
	}

	for _, modelID := range validModels {
		model, err := prov.LanguageModel(modelID)
		if err != nil {
			t.Errorf("Expected model '%s' to be valid, got error: %v", modelID, err)
		}
		if model == nil {
			t.Errorf("Expected model '%s' to be created", modelID)
		}
	}

}

func TestVideoModelValidation(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)

	// Alibaba's TypeScript SDK video model ID type allows current known IDs
	// and custom strings (wan3 ships new ids on demand), so Provider.VideoModel
	// accepts any non-empty model ID, matching Provider.LanguageModel.
	validModels := []string{
		"wan2.5-t2v-preview",
		"wan2.6-t2v",
		"wan2.6-i2v",
		"wan2.6-i2v-flash",
		"wan2.6-r2v",
		"wan2.6-r2v-flash",
		"wan2.7-t2v",
		"wan3.0-video",
		"custom-video-model-id",
	}

	for _, modelID := range validModels {
		model, err := prov.VideoModel(modelID)
		if err != nil {
			t.Errorf("Expected video model '%s' to be valid, got error: %v", modelID, err)
		}
		if model == nil {
			t.Errorf("Expected video model '%s' to be created", modelID)
		}
		if model.SpecificationVersion() != "v4" {
			t.Errorf("Expected spec version 'v4', got '%s'", model.SpecificationVersion())
		}
	}
}

func TestConfigValidation(t *testing.T) {
	// Valid config
	cfg := Config{APIKey: "test-key"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	// Invalid config (empty API key)
	invalidCfg := Config{APIKey: ""}
	if err := invalidCfg.Validate(); err == nil {
		t.Error("Expected error for empty API key")
	}
}

func TestAlibabaUsageConversion(t *testing.T) {
	usage := AlibabaUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	converted := ConvertAlibabaUsage(usage)

	if converted.InputTokens == nil || *converted.InputTokens != 100 {
		t.Errorf("Expected InputTokens to be 100, got %v", converted.InputTokens)
	}

	if converted.OutputTokens == nil || *converted.OutputTokens != 50 {
		t.Errorf("Expected OutputTokens to be 50, got %v", converted.OutputTokens)
	}

	if converted.TotalTokens == nil || *converted.TotalTokens != 150 {
		t.Errorf("Expected TotalTokens to be 150, got %v", converted.TotalTokens)
	}

	// With no cache/reasoning details reported, noCache should equal the full
	// prompt token count and cache/reasoning should be zero (TS always
	// reports the full nested shape).
	if converted.InputDetails == nil || converted.InputDetails.NoCacheTokens == nil || *converted.InputDetails.NoCacheTokens != 100 {
		t.Errorf("Expected NoCacheTokens to be 100, got %+v", converted.InputDetails)
	}
}

func TestAlibabaUsageWithCaching(t *testing.T) {
	usage := AlibabaUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		PromptTokensDetails: &AlibabaPromptTokensDetails{
			CachedTokens:             80,
			CacheCreationInputTokens: 20,
		},
	}

	converted := ConvertAlibabaUsage(usage)

	if converted.InputDetails == nil {
		t.Fatal("Expected InputDetails to be set")
	}

	if converted.InputDetails.CacheReadTokens == nil || *converted.InputDetails.CacheReadTokens != 80 {
		t.Errorf("Expected CacheReadTokens to be 80, got %v", converted.InputDetails.CacheReadTokens)
	}

	if converted.InputDetails.CacheWriteTokens == nil || *converted.InputDetails.CacheWriteTokens != 20 {
		t.Errorf("Expected CacheWriteTokens to be 20, got %v", converted.InputDetails.CacheWriteTokens)
	}

	// Alibaba counts cache reads/writes inside prompt_tokens, so noCache
	// subtracts both.
	if converted.InputDetails.NoCacheTokens == nil || *converted.InputDetails.NoCacheTokens != 0 {
		t.Errorf("Expected NoCacheTokens to be 0, got %v", converted.InputDetails.NoCacheTokens)
	}
}

func TestAlibabaUsageWithThinking(t *testing.T) {
	usage := AlibabaUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CompletionTokensDetails: &AlibabaCompletionTokensDetails{
			ReasoningTokens: 30,
		},
	}

	converted := ConvertAlibabaUsage(usage)

	if converted.OutputDetails == nil {
		t.Fatal("Expected OutputDetails to be set")
	}

	if converted.OutputDetails.ReasoningTokens == nil || *converted.OutputDetails.ReasoningTokens != 30 {
		t.Errorf("Expected ReasoningTokens to be 30, got %v", converted.OutputDetails.ReasoningTokens)
	}

	// text = completion - reasoning, clamped at 0.
	if converted.OutputDetails.TextTokens == nil || *converted.OutputDetails.TextTokens != 20 {
		t.Errorf("Expected TextTokens to be 20, got %v", converted.OutputDetails.TextTokens)
	}
}

func TestAlibabaUsageRawPreservesUndeclaredFields(t *testing.T) {
	var usage AlibabaUsage
	raw := []byte(`{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"some_future_field":42}`)
	if err := json.Unmarshal(raw, &usage); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	converted := ConvertAlibabaUsage(usage)
	if converted.Raw == nil {
		t.Fatal("expected Raw to be populated")
	}
	if v, ok := converted.Raw["some_future_field"]; !ok || v != float64(42) {
		t.Errorf("expected Raw to preserve some_future_field, got %v", converted.Raw)
	}
}

func TestAlibabaUsageTextTokensClampedAtZero(t *testing.T) {
	// Defensive: reasoning tokens should never exceed completion tokens in
	// practice, but the conversion must not report a negative text count.
	usage := AlibabaUsage{
		CompletionTokens: 10,
		CompletionTokensDetails: &AlibabaCompletionTokensDetails{
			ReasoningTokens: 15,
		},
	}
	converted := ConvertAlibabaUsage(usage)
	if converted.OutputDetails == nil || converted.OutputDetails.TextTokens == nil || *converted.OutputDetails.TextTokens != 0 {
		t.Errorf("expected TextTokens to be clamped to 0, got %+v", converted.OutputDetails)
	}
}
