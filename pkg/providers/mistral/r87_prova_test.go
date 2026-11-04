package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from mistral-chat-language-model.test.ts (TS 9ab42ea99a, #24350):
// Mistral Large 4 accepts low/medium/high reasoning_effort and cannot disable
// reasoning.
func TestR87provaLarge4ReasoningEffort(t *testing.T) {
	cases := []struct {
		level types.ReasoningLevel
		want  string
	}{
		{types.ReasoningMinimal, "low"}, {types.ReasoningLow, "low"},
		{types.ReasoningMedium, "medium"}, {types.ReasoningHigh, "high"},
		{types.ReasoningXHigh, "high"}, {types.ReasoningMax, "high"},
	}
	for _, id := range []string{ModelMistralLarge4, ModelMistralLarge40} {
		m := NewLanguageModel(New(Config{APIKey: "k"}), id)
		for _, c := range cases {
			level := c.level
			body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level, Prompt: types.Prompt{Text: "x"}}, false)
			if body["reasoning_effort"] != c.want {
				t.Errorf("%s %s: reasoning_effort = %v, want %s", id, level, body["reasoning_effort"], c.want)
			}
		}
		medium := types.ReasoningMedium
		if ws := m.checkReasoningWarnings(&provider.GenerateOptions{Reasoning: &medium, Prompt: types.Prompt{Text: "x"}}); len(ws) != 0 {
			t.Errorf("%s medium: warnings = %#v, want none", id, ws)
		}

		// Native medium reasoning effort passes validation, is sent, no warnings.
		native := &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "x"},
			ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"reasoningEffort": "medium"}},
		}
		if err := validateChatProviderOptions(native.ProviderOptions); err != nil {
			t.Fatalf("%s: native medium rejected: %v", id, err)
		}
		if _, err := extractMistralProviderOptions(native); err != nil {
			t.Fatalf("%s: native medium rejected: %v", id, err)
		}
		if body := m.buildRequestBody(native, false); body["reasoning_effort"] != "medium" {
			t.Errorf("%s native medium: reasoning_effort = %v", id, body["reasoning_effort"])
		}
		if ws := m.checkReasoningWarnings(native); len(ws) != 0 {
			t.Errorf("%s native medium: warnings = %#v", id, ws)
		}

		none := types.ReasoningNone
		opts := &provider.GenerateOptions{Reasoning: &none, Prompt: types.Prompt{Text: "x"}}
		if body := m.buildRequestBody(opts, false); body["reasoning_effort"] != "low" {
			t.Errorf("%s none: reasoning_effort = %v, want low", id, body["reasoning_effort"])
		}
		ws := m.checkReasoningWarnings(opts)
		if len(ws) != 1 || ws[0].Type != "compatibility" || ws[0].Feature != "reasoning" ||
			ws[0].Details != `reasoning "none" is not directly supported by this model. mapped to effort "low".` {
			t.Errorf("%s none: warnings = %#v", id, ws)
		}
	}
}
