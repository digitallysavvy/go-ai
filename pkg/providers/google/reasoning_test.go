package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func makeTestGoogleProvider() *Provider {
	return New(Config{APIKey: "test-key"})
}

func TestGoogleReasoningNoneDisablesThinking(t *testing.T) {
	prov := makeTestGoogleProvider()
	model := NewLanguageModel(prov, "gemini-2.5-pro")

	level := types.ReasoningNone
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts)

	genConfig, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected generationConfig, got: %v", body["generationConfig"])
	}
	tc, ok := genConfig["thinkingConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinkingConfig, got: %v", genConfig["thinkingConfig"])
	}
	if tc["thinkingBudget"] != 0 {
		t.Errorf("expected thinkingBudget 0 for none, got: %v", tc["thinkingBudget"])
	}
}

func TestGoogleReasoningDefaultOmitsThinking(t *testing.T) {
	prov := makeTestGoogleProvider()
	model := NewLanguageModel(prov, "gemini-2.5-pro")

	level := types.ReasoningDefault
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts)

	genConfig, _ := body["generationConfig"].(map[string]interface{})
	if _, hasTC := genConfig["thinkingConfig"]; hasTC {
		t.Error("expected no thinkingConfig for provider-default")
	}
}

func TestGoogleReasoningNilOmitsThinking(t *testing.T) {
	prov := makeTestGoogleProvider()
	model := NewLanguageModel(prov, "gemini-2.5-pro")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts)

	genConfig, _ := body["generationConfig"].(map[string]interface{})
	if _, hasTC := genConfig["thinkingConfig"]; hasTC {
		t.Error("expected no thinkingConfig when Reasoning is nil")
	}
}

func TestGoogleReasoningDynamicBudget(t *testing.T) {
	prov := makeTestGoogleProvider()
	// gemini-2.5-pro max thinking = 32768
	model := NewLanguageModel(prov, "gemini-2.5-pro")

	tests := []struct {
		level         types.ReasoningLevel
		maxOutputTok  int
		wantBudgetMin int
		wantBudgetMax int
	}{
		// minimal = 5% of 32768 ≈ 1638
		{types.ReasoningMinimal, 0, 1500, 1800},
		// medium = 40% of 32768 ≈ 13107
		{types.ReasoningMedium, 0, 12000, 14000},
		// high = 70% of 32768 ≈ 22937
		{types.ReasoningHigh, 0, 22000, 24000},
		// xhigh = 100% of 32768 = 32768
		{types.ReasoningXHigh, 0, 32768, 32768},
		// medium capped by maxOutputTokens=1000 → 40% of 1000 = 400
		{types.ReasoningMedium, 1000, 400, 400},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			if tt.maxOutputTok > 0 {
				opts.MaxTokens = &tt.maxOutputTok
			}
			body := model.buildRequestBody(opts)

			genConfig, ok := body["generationConfig"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected generationConfig, got: %v", body["generationConfig"])
			}
			tc, ok := genConfig["thinkingConfig"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected thinkingConfig, got: %v", genConfig["thinkingConfig"])
			}
			budget, ok := tc["thinkingBudget"].(int)
			if !ok {
				t.Fatalf("expected int thinkingBudget, got: %T %v", tc["thinkingBudget"], tc["thinkingBudget"])
			}
			if budget < tt.wantBudgetMin || budget > tt.wantBudgetMax {
				t.Errorf("thinkingBudget %d out of expected range [%d, %d]",
					budget, tt.wantBudgetMin, tt.wantBudgetMax)
			}
		})
	}
}

func TestGoogleReasoningMinIsAtLeastOne(t *testing.T) {
	prov := makeTestGoogleProvider()
	// Use a model with small max thinking to stress the min=1 floor
	model := NewLanguageModel(prov, "unknown-model")

	// minimal = 5% of 8192 = 409 — still > 1, but let's verify non-zero
	level := types.ReasoningMinimal
	maxTok := 1
	opts := &provider.GenerateOptions{Reasoning: &level, MaxTokens: &maxTok}
	body := model.buildRequestBody(opts)

	genConfig, _ := body["generationConfig"].(map[string]interface{})
	tc, _ := genConfig["thinkingConfig"].(map[string]interface{})
	budget := tc["thinkingBudget"].(int)
	if budget < 1 {
		t.Errorf("thinkingBudget must be >= 1, got %d", budget)
	}
}

func TestMaxThinkingTokensForModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    int
	}{
		{"gemini-2.5-pro", 32768},
		{"gemini-2.5-pro-exp-0827", 32768},
		{"gemini-2.5-flash", 24576},
		{"gemini-2.0-flash-thinking-exp", 8192},
		{"gemini-1.5-pro", 8192},
		{"unknown", 8192},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := maxThinkingTokensForModel(tt.modelID)
			if got != tt.want {
				t.Errorf("maxThinkingTokensForModel(%q) = %d, want %d", tt.modelID, got, tt.want)
			}
		})
	}
}
