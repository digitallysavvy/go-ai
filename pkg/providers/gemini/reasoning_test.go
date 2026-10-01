package gemini

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// makeTestModel creates a LanguageModel with a minimal test Config.
func makeTestModel(modelID string) *LanguageModel {
	return NewLanguageModel(Config{
		ProviderName:        "google",
		MetadataKey:         "google",
		ProviderOptionsKeys: []string{"google"},
	}, modelID)
}

// makeVertexTestModel creates a LanguageModel with Vertex-style Config to test
// the ProviderOptionsKeys fallback chain.
func makeVertexTestModel(modelID string) *LanguageModel {
	return NewLanguageModel(Config{
		ProviderName:        "google-vertex",
		MetadataKey:         "vertex",
		ProviderOptionsKeys: []string{"vertex", "googleVertex", "google"},
	}, modelID)
}

// --- thinkingConfig from call-level Reasoning --------------------------------

func TestReasoningNoneDisablesThinking(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	level := types.ReasoningNone
	body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level}, false)

	genConfig := body["generationConfig"].(map[string]interface{})
	tc := genConfig["thinkingConfig"].(map[string]interface{})
	if tc["thinkingBudget"] != 0 {
		t.Errorf("thinkingBudget: got %v, want 0", tc["thinkingBudget"])
	}
}

func TestReasoningDefaultOmitsThinking(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	level := types.ReasoningDefault
	body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level}, false)

	genConfig, _ := body["generationConfig"].(map[string]interface{})
	if _, has := genConfig["thinkingConfig"]; has {
		t.Error("expected no thinkingConfig for provider-default")
	}
}

func TestReasoningNilOmitsThinking(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	body := m.buildRequestBody(&provider.GenerateOptions{}, false)

	genConfig, _ := body["generationConfig"].(map[string]interface{})
	if _, has := genConfig["thinkingConfig"]; has {
		t.Error("expected no thinkingConfig when Reasoning is nil")
	}
}

// TS google-language-model.test.ts "Gemini 2.5 models (thinkingBudget)":
// budget = min(modelMax, round(65536 * pct)).
func TestReasoningDynamicBudget(t *testing.T) {
	cases := []struct {
		model string
		level types.ReasoningLevel
		want  int
	}{
		{"gemini-2.5-pro", types.ReasoningMinimal, 1311},  // round(65536*0.02)
		{"gemini-2.5-pro", types.ReasoningLow, 6554},      // round(65536*0.1)
		{"gemini-2.5-pro", types.ReasoningMedium, 19661},  // round(65536*0.3)
		{"gemini-2.5-pro", types.ReasoningHigh, 32768},    // clamped to 2.5-pro max
		{"gemini-2.5-pro", types.ReasoningXHigh, 32768},   // clamped to 2.5-pro max
		{"gemini-2.5-flash-lite", types.ReasoningMedium, 19661},
		{"gemini-2.5-flash", types.ReasoningHigh, 24576}, // clamped to flash max
	}
	for _, tt := range cases {
		t.Run(tt.model+"/"+string(tt.level), func(t *testing.T) {
			level := tt.level
			body := makeTestModel(tt.model).buildRequestBody(&provider.GenerateOptions{Reasoning: &level}, false)
			tc := body["generationConfig"].(map[string]interface{})["thinkingConfig"].(map[string]interface{})
			if tc["thinkingBudget"] != tt.want {
				t.Errorf("thinkingBudget = %v, want %d", tc["thinkingBudget"], tt.want)
			}
		})
	}
}

// TS: "should use providerOptions thinkingConfig when both reasoning and providerOptions are set".
func TestReasoningProviderOptionsOverrideResolved(t *testing.T) {
	level := types.ReasoningHigh
	body := makeTestModel("gemini-2.5-pro").buildRequestBody(&provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingBudget": 999}},
		},
	}, false)
	tc := body["generationConfig"].(map[string]interface{})["thinkingConfig"].(map[string]interface{})
	if tc["thinkingBudget"] != 999 {
		t.Errorf("thinkingBudget = %v, want 999", tc["thinkingBudget"])
	}
}

// TS table: "should map reasoning $reasoning to thinkingLevel $expectedThinkingLevel for $modelId" (f69920a).
func TestReasoningGemini3MinimumThinkingLevel(t *testing.T) {
	cases := []struct {
		model string
		level types.ReasoningLevel
		want  string
		warn  bool
	}{
		{"gemini-3-pro-preview", types.ReasoningMinimal, "minimal", false},
		{"gemini-3-pro-preview", types.ReasoningNone, "minimal", false},
		{"gemini-3-pro-preview", types.ReasoningXHigh, "high", true},
		{"gemini-3.1-pro-preview", types.ReasoningMedium, "medium", false},
		{"gemini-3.7-flash", types.ReasoningMinimal, "low", true},
		{"gemini-3.7-flash", types.ReasoningNone, "low", false},
		{"gemini-3.7-flash-video-understanding-eap", types.ReasoningNone, "low", false},
		{"gemini-flash-latest", types.ReasoningMinimal, "low", true},
		{"models/gemini-3.7-flash", types.ReasoningMinimal, "low", true},
		{"gemini-3.8-flash", types.ReasoningMinimal, "low", true},
		{"gemini-3.10-flash-preview", types.ReasoningMinimal, "low", true},
		{"gemini-4.0-flash", types.ReasoningMinimal, "low", true},
		{"gemini-3-flash-preview", types.ReasoningMinimal, "minimal", false},
		{"gemini-3.6-flash", types.ReasoningMinimal, "minimal", false},
		{"gemini-3.7-flash-lite", types.ReasoningMinimal, "minimal", false},
		{"gemini-3.10-flash-lite-preview", types.ReasoningMinimal, "minimal", false},
		{"gemini-flash-lite-latest", types.ReasoningMinimal, "minimal", false},
		{"gemini-3.1-flash-image-preview", types.ReasoningHigh, "high", false},
	}
	for _, tt := range cases {
		t.Run(tt.model+"/"+string(tt.level), func(t *testing.T) {
			level := tt.level
			body, _, warnings, err := makeTestModel(tt.model).buildRequest(context.Background(), &provider.GenerateOptions{Reasoning: &level}, false)
			if err != nil {
				t.Fatal(err)
			}
			tc := body["generationConfig"].(map[string]interface{})["thinkingConfig"].(map[string]interface{})
			if tc["thinkingLevel"] != tt.want {
				t.Errorf("thinkingLevel = %v, want %s", tc["thinkingLevel"], tt.want)
			}
			hasCompat := false
			for _, w := range warnings {
				if w.Type == "compatibility" && w.Feature == "reasoning" {
					hasCompat = true
				}
			}
			if hasCompat != tt.warn {
				t.Errorf("compatibility warning = %v, want %v (%#v)", hasCompat, tt.warn, warnings)
			}
		})
	}
}

// --- thinkingConfig from provider options ------------------------------------

func TestProviderOptionsThinkingConfig_Google(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	body := m.buildRequestBody(&provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget":  1000,
					"includeThoughts": true,
				},
			},
		},
	}, false)

	genConfig := body["generationConfig"].(map[string]interface{})
	tc := genConfig["thinkingConfig"].(map[string]interface{})
	if tc["thinkingBudget"] != 1000 {
		t.Errorf("thinkingBudget: got %v, want 1000", tc["thinkingBudget"])
	}
	if tc["includeThoughts"] != true {
		t.Errorf("includeThoughts: got %v, want true", tc["includeThoughts"])
	}
}

func TestProviderOptionsThinkingConfig_VertexFallbackChain(t *testing.T) {
	m := makeVertexTestModel("gemini-2.5-pro")

	// "vertex" key should take precedence.
	body := m.buildRequestBody(&provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"vertex": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingBudget": 500},
			},
			"google": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingBudget": 999},
			},
		},
	}, false)
	tc := body["generationConfig"].(map[string]interface{})["thinkingConfig"].(map[string]interface{})
	if tc["thinkingBudget"] != 500 {
		t.Errorf("expected vertex key to win, got thinkingBudget=%v", tc["thinkingBudget"])
	}

	// "googleVertex" legacy key is tried when "vertex" absent.
	body2 := m.buildRequestBody(&provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{
			"googleVertex": map[string]interface{}{
				"thinkingConfig": map[string]interface{}{"thinkingBudget": 200},
			},
		},
	}, false)
	tc2 := body2["generationConfig"].(map[string]interface{})["thinkingConfig"].(map[string]interface{})
	if tc2["thinkingBudget"] != 200 {
		t.Errorf("expected googleVertex key to match, got thinkingBudget=%v", tc2["thinkingBudget"])
	}
}

// --- maxThinkingTokensForModel -----------------------------------------------

// TS getMaxThinkingTokensForGemini25Model.
func TestMaxThinkingTokensForModel(t *testing.T) {
	cases := []struct {
		modelID string
		want    int
	}{
		{"gemini-2.5-pro", 32768},
		{"gemini-2.5-pro-exp-0827", 32768},
		{"gemini-3-pro-image-preview", 32768},
		{"gemini-3.1-flash-image-preview", 24576},
		{"gemini-2.5-flash", 24576},
		{"unknown", 24576},
	}
	for _, tt := range cases {
		if got := maxThinkingTokensForModel(tt.modelID); got != tt.want {
			t.Errorf("maxThinkingTokensForModel(%q) = %d, want %d", tt.modelID, got, tt.want)
		}
	}
}

// gemini-3-pro-image models use thinkingBudget (TS excludes only gemini-3-pro-image).
func TestGemini3ProImageModelUsesThinkingBudget(t *testing.T) {
	level := types.ReasoningHigh
	body := makeTestModel("gemini-3-pro-image-preview").buildRequestBody(&provider.GenerateOptions{Reasoning: &level}, false)
	tc := body["generationConfig"].(map[string]interface{})["thinkingConfig"].(map[string]interface{})
	if _, hasLevel := tc["thinkingLevel"]; hasLevel {
		t.Errorf("gemini-3-pro-image must not use thinkingLevel: %#v", tc)
	}
	if tc["thinkingBudget"] != 32768 {
		t.Errorf("thinkingBudget = %v, want 32768", tc["thinkingBudget"])
	}
}

// --- isGemini3Model / isGemmaModel -------------------------------------------

func TestIsGemini3Model(t *testing.T) {
	yes := []string{"gemini-3.0-pro", "gemini-3-flash", "gemini-99-pro-preview", "gemini-ultra-latest"}
	no := []string{"gemini-2.5-pro", "gemini-1.5-flash", "gemma-7b"}
	for _, id := range yes {
		if !isGemini3Model(id) {
			t.Errorf("isGemini3Model(%q) = false, want true", id)
		}
	}
	for _, id := range no {
		if isGemini3Model(id) {
			t.Errorf("isGemini3Model(%q) = true, want false", id)
		}
	}
}

func TestIsGemmaModel(t *testing.T) {
	yes := []string{"gemma-7b", "gemma-2-9b-it"}
	no := []string{"gemini-pro", "gemini-2.5-pro"}
	for _, id := range yes {
		if !isGemmaModel(id) {
			t.Errorf("isGemmaModel(%q) = false, want true", id)
		}
	}
	for _, id := range no {
		if isGemmaModel(id) {
			t.Errorf("isGemmaModel(%q) = true, want false", id)
		}
	}
}
