package alibaba

import (
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
		"qwen-qwq-32b-preview",
		"qwen-vl-max",
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

	// Test invalid model
	_, err := prov.LanguageModel("invalid-model")
	if err == nil {
		t.Error("Expected error for invalid model")
	}
}

func TestVideoModelValidation(t *testing.T) {
	cfg := Config{APIKey: "test-key"}
	prov := New(cfg)

	validModels := []string{
		"wan2.5-t2v",
		"wan2.6-t2v",
		"wan2.6-i2v",
		"wan2.6-i2v-flash",
		"wan2.6-r2v",
		"wan2.6-r2v-flash",
	}

	for _, modelID := range validModels {
		model, err := prov.VideoModel(modelID)
		if err != nil {
			t.Errorf("Expected video model '%s' to be valid, got error: %v", modelID, err)
		}
		if model == nil {
			t.Errorf("Expected video model '%s' to be created", modelID)
		}
		if model.SpecificationVersion() != "v3" {
			t.Errorf("Expected spec version 'v3', got '%s'", model.SpecificationVersion())
		}
	}

	// Test invalid model
	_, err := prov.VideoModel("invalid-video-model")
	if err == nil {
		t.Error("Expected error for invalid video model")
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
}

func TestAlibabaUsageWithCaching(t *testing.T) {
	cacheHit := 80
	cacheMiss := 20

	usage := AlibabaUsage{
		PromptTokens:          100,
		CompletionTokens:      50,
		TotalTokens:           150,
		PromptCacheHitTokens:  &cacheHit,
		PromptCacheMissTokens: &cacheMiss,
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
}

func TestAlibabaUsageWithThinking(t *testing.T) {
	thinkingTokens := 30

	usage := AlibabaUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		ThinkingTokens:   &thinkingTokens,
	}

	converted := ConvertAlibabaUsage(usage)

	if converted.OutputDetails == nil {
		t.Fatal("Expected OutputDetails to be set")
	}

	if converted.OutputDetails.ReasoningTokens == nil || *converted.OutputDetails.ReasoningTokens != 30 {
		t.Errorf("Expected ReasoningTokens to be 30, got %v", converted.OutputDetails.ReasoningTokens)
	}
}
