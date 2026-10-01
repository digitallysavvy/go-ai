package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDeepSeekThinkingSuppressesTemperatureAndTopP guards TS
// deepseek-chat-language-model.ts:322-345: temperature/topP are omitted from
// the wire body (with "unsupported" warnings) whenever DeepSeek thinking is
// enabled — including the default case where no explicit reasoning/thinking
// option is set but the model is deepseek-reasoner or a V4 model.
func TestDeepSeekThinkingSuppressesTemperatureAndTopP(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	temp := 0.7
	topP := 0.9

	t.Run("deepseek-reasoner default thinking suppresses both", func(t *testing.T) {
		model := NewLanguageModel(p, "deepseek-reasoner")
		body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:      types.Prompt{Text: "hi"},
			Temperature: &temp,
			TopP:        &topP,
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := body["temperature"]; ok {
			t.Errorf("temperature = %#v, want omitted", body["temperature"])
		}
		if _, ok := body["top_p"]; ok {
			t.Errorf("top_p = %#v, want omitted", body["top_p"])
		}
		var sawTemp, sawTopP bool
		for _, w := range warnings {
			if w.Feature == "temperature" {
				sawTemp = true
			}
			if w.Feature == "topP" {
				sawTopP = true
			}
		}
		if !sawTemp || !sawTopP {
			t.Fatalf("warnings = %#v, want temperature and topP unsupported warnings", warnings)
		}
	})

	t.Run("deepseek-chat non-reasoner keeps both", func(t *testing.T) {
		model := NewLanguageModel(p, "deepseek-chat")
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:      types.Prompt{Text: "hi"},
			Temperature: &temp,
			TopP:        &topP,
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["temperature"] != temp || body["top_p"] != topP {
			t.Fatalf("body = %#v, want temperature/top_p forwarded", body)
		}
	})

	t.Run("thinking explicitly disabled keeps both", func(t *testing.T) {
		model := NewLanguageModel(p, "deepseek-reasoner")
		none := types.ReasoningNone
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:      types.Prompt{Text: "hi"},
			Reasoning:   &none,
			Temperature: &temp,
			TopP:        &topP,
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["temperature"] != temp || body["top_p"] != topP {
			t.Fatalf("body = %#v, want temperature/top_p forwarded when thinking is disabled", body)
		}
	})
}

func TestDeepSeekReasoningAllLevels(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	tests := []struct {
		level       types.ReasoningLevel
		wantType    string
		wantEffort  string
		hasThinking bool
	}{
		{types.ReasoningNone, "disabled", "", true},
		{types.ReasoningMinimal, "enabled", "low", true},
		{types.ReasoningLow, "enabled", "low", true},
		// TS deepseek-chat-language-model.ts effortMap maps medium -> "high"
		// (DeepSeek's reasoning_effort scale is low/high/max, no "medium" value).
		{types.ReasoningMedium, "enabled", "high", true},
		{types.ReasoningHigh, "enabled", "high", true},
		{types.ReasoningXHigh, "enabled", "max", true},
		{types.ReasoningDefault, "", "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			level := tt.level
			opts := &provider.GenerateOptions{Reasoning: &level}
			body := model.buildRequestBody(opts, false)
			if _, ok := body["stream"]; ok {
				t.Fatalf("stream = %#v, want omitted for non-streaming request", body["stream"])
			}

			thinkingRaw, hasKey := body["thinking"]
			if hasKey != tt.hasThinking {
				t.Fatalf("thinking presence: want %v, got %v", tt.hasThinking, hasKey)
			}
			if tt.hasThinking {
				thinking, ok := thinkingRaw.(map[string]interface{})
				if !ok {
					t.Fatalf("thinking should be map[string]interface{}, got %T", thinkingRaw)
				}
				if thinking["type"] != tt.wantType {
					t.Errorf("thinking.type: want %q, got %v", tt.wantType, thinking["type"])
				}
			}
			if tt.wantEffort == "" {
				if _, ok := body["reasoning_effort"]; ok {
					t.Error("reasoning_effort should be omitted")
				}
			} else if body["reasoning_effort"] != tt.wantEffort {
				t.Errorf("reasoning_effort: want %q, got %v", tt.wantEffort, body["reasoning_effort"])
			}
		})
	}
}

func TestDeepSeekReasoningNilOmitted(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-chat")

	opts := &provider.GenerateOptions{}
	body := model.buildRequestBody(opts, false)

	if _, ok := body["thinking"]; ok {
		t.Error("expected no thinking when Reasoning is nil")
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("expected no reasoning_effort when Reasoning is nil")
	}
}

func TestDeepSeekReasoningPropagated(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body := model.buildRequestBody(opts, false)

	thinkingRaw, ok := body["thinking"]
	if !ok {
		t.Fatal("expected thinking to be set for ReasoningHigh")
	}
	thinking, ok := thinkingRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("thinking should be map[string]interface{}, got %T", thinkingRaw)
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type 'enabled', got: %v", thinking["type"])
	}
}

func TestDeepSeekProviderOptionsOverrideReasoning(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"deepseek": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "disabled"},
			},
		},
	}
	body, warnings, err := model.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
	}

	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for camelCase provider options, got %#v", warnings)
	}
	thinking := body["thinking"].(map[string]interface{})
	if thinking["type"] != "disabled" {
		t.Errorf("provider options should override top-level Reasoning; got thinking.type=%v", thinking["type"])
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort must be omitted when thinking is disabled: %v", body["reasoning_effort"])
	}
}

func TestDeepSeekProviderOptionsThinkingTypesPassThrough(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	tests := []struct {
		thinkingType string
		wantType     string
		wantWarnings int
	}{
		// "adaptive" is a legacy value mapped to "enabled" with a compatibility warning.
		{"adaptive", "enabled", 1},
		{"enabled", "enabled", 0},
		{"disabled", "disabled", 0},
	}

	for _, tt := range tests {
		t.Run(tt.thinkingType, func(t *testing.T) {
			body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
				ProviderOptions: map[string]interface{}{
					"deepseek": map[string]interface{}{
						"thinking": map[string]interface{}{"type": tt.thinkingType},
					},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
			}
			if len(warnings) != tt.wantWarnings {
				t.Fatalf("warnings = %#v, want %d", warnings, tt.wantWarnings)
			}
			thinking, ok := body["thinking"].(map[string]interface{})
			if !ok {
				t.Fatalf("thinking = %T, want map", body["thinking"])
			}
			if thinking["type"] != tt.wantType {
				t.Fatalf("thinking.type = %v, want %q", thinking["type"], tt.wantType)
			}
		})
	}
}

func TestDeepSeekProviderOptionsReasoningEffort(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"deepseek": map[string]interface{}{
				"reasoningEffort": "max",
			},
		},
	}
	body, warnings, err := model.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for reasoningEffort option, got %#v", warnings)
	}
	if body["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %v, want max", body["reasoning_effort"])
	}
}

func TestDeepSeekReasoningCompatibilityWarnings(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	tests := []struct {
		name       string
		level      types.ReasoningLevel
		wantDetail string
	}{
		{
			name:       "minimal",
			level:      types.ReasoningMinimal,
			wantDetail: `reasoning "minimal" is not directly supported by this model. mapped to effort "low".`,
		},
		{
			name:       "medium",
			level:      types.ReasoningMedium,
			wantDetail: `reasoning "medium" is not directly supported by this model. mapped to effort "high".`,
		},
		{
			name:       "xhigh",
			level:      types.ReasoningXHigh,
			wantDetail: `reasoning "xhigh" is not directly supported by this model. mapped to effort "max".`,
		},
		{
			name:       "medium",
			level:      types.ReasoningMedium,
			wantDetail: `reasoning "medium" is not directly supported by this model. mapped to effort "high".`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Reasoning: &tt.level}, false)
			if err != nil {
				t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
			}
			if len(warnings) != 1 {
				t.Fatalf("warnings = %#v, want one compatibility warning", warnings)
			}
			if warnings[0].Type != "compatibility" || warnings[0].Feature != "reasoning" || warnings[0].Details != tt.wantDetail {
				t.Fatalf("warning = %#v, want compatibility reasoning detail %q", warnings[0], tt.wantDetail)
			}
		})
	}
}

func TestDeepSeekProviderReasoningEffortSuppressesMappingWarning(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")
	level := types.ReasoningXHigh
	_, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"deepseek": map[string]interface{}{"reasoningEffort": "max"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBodyWithWarnings error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none when provider reasoningEffort is explicit", warnings)
	}
}
