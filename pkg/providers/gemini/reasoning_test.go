package gemini

import (
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

func TestReasoningDynamicBudget(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro") // max thinking = 32768

	cases := []struct {
		level         types.ReasoningLevel
		maxOut        int
		wantMin, wantMax int
	}{
		{types.ReasoningMinimal, 0, 1024, 1024},   // 2% of 32768 = 655, floored to 1024
		{types.ReasoningMedium, 0, 9000, 10500},   // 30% of 32768 ≈ 9830
		{types.ReasoningHigh, 0, 19000, 20000},    // 60% of 32768 ≈ 19660
		{types.ReasoningXHigh, 0, 29000, 29500},   // 90% of 32768 ≈ 29491
		{types.ReasoningMedium, 1000, 1024, 1024}, // 30% of 1000 = 300, floored to 1024
	}

	for _, tt := range cases {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			if tt.maxOut > 0 {
				opts.MaxTokens = &tt.maxOut
			}
			body := m.buildRequestBody(opts, false)

			genConfig := body["generationConfig"].(map[string]interface{})
			tc := genConfig["thinkingConfig"].(map[string]interface{})
			budget := tc["thinkingBudget"].(int)
			if budget < tt.wantMin || budget > tt.wantMax {
				t.Errorf("thinkingBudget %d outside [%d, %d]", budget, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestReasoningMinIsAtLeast1024(t *testing.T) {
	m := makeTestModel("unknown-model") // 2% of 8192 = 163 with maxOut=1 → floored to 1024
	level := types.ReasoningMinimal
	maxTok := 1
	body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level, MaxTokens: &maxTok}, false)

	genConfig := body["generationConfig"].(map[string]interface{})
	tc := genConfig["thinkingConfig"].(map[string]interface{})
	if tc["thinkingBudget"].(int) < 1024 {
		t.Errorf("thinkingBudget must be >= 1024, got %d", tc["thinkingBudget"].(int))
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

func TestMaxThinkingTokensForModel(t *testing.T) {
	cases := []struct{ modelID string; want int }{
		{"gemini-2.5-pro", 32768},
		{"gemini-2.5-pro-exp-0827", 32768},
		{"gemini-3-pro-image-preview", 32768},   // TS SDK: id.includes('gemini-3-pro-image')
		{"gemini-3.1-flash-image-preview", 8192}, // does NOT match gemini-3-pro-image
		{"gemini-2.5-flash", 24576},
		{"gemini-2.0-flash-thinking-exp", 8192},
		{"gemini-1.5-pro", 8192},
		{"unknown", 8192},
	}
	for _, tt := range cases {
		t.Run(tt.modelID, func(t *testing.T) {
			got := maxThinkingTokensForModel(tt.modelID)
			if got != tt.want {
				t.Errorf("maxThinkingTokensForModel(%q) = %d, want %d", tt.modelID, got, tt.want)
			}
		})
	}
}

// --- Gemini 3 image model thinkingLevel exclusion ---------------------------

func TestGemini3ImageModelDoesNotUseThinkingLevel(t *testing.T) {
	imageModels := []string{
		"gemini-3-pro-image-preview",
		"gemini-3.1-flash-image-preview",
	}
	for _, id := range imageModels {
		m := makeTestModel(id)
		level := types.ReasoningHigh
		body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level}, false)
		gc, _ := body["generationConfig"].(map[string]interface{})
		tc, hasTc := gc["thinkingConfig"].(map[string]interface{})
		if !hasTc {
			continue
		}
		if _, hasLevel := tc["thinkingLevel"]; hasLevel {
			t.Errorf("model %q (image) must NOT use thinkingLevel", id)
		}
	}
}

// --- isGemini3Model / isGemmaModel -------------------------------------------

func TestIsGemini3Model(t *testing.T) {
	yes := []string{"gemini-3", "gemini-3.0-pro", "gemini-3-flash"}
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
