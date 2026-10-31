package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from mistral-chat-language-model.test.ts (TS #24062): GLM-5.3 models
// accept only low/high/max reasoning_effort and cannot disable reasoning.
func TestGLM53ReasoningEffort(t *testing.T) {
	ids := []string{"zai-glm-5-3", "zai-glm-5", "zai-glm-latest", "zai-glm-5-3-flash"}
	cases := map[types.ReasoningLevel]string{
		types.ReasoningMinimal: "low", types.ReasoningLow: "low",
		types.ReasoningMedium: "high", types.ReasoningHigh: "high",
		types.ReasoningXHigh: "max", types.ReasoningMax: "max",
	}
	for _, id := range ids {
		m := NewLanguageModel(New(Config{APIKey: "k"}), id)
		for level, want := range cases {
			level := level
			body := m.buildRequestBody(&provider.GenerateOptions{Reasoning: &level, Prompt: types.Prompt{Text: "x"}}, false)
			if body["reasoning_effort"] != want {
				t.Errorf("%s %s: reasoning_effort = %v, want %s", id, level, body["reasoning_effort"], want)
			}
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

	// Provider option accepts low and max.
	m := NewLanguageModel(New(Config{APIKey: "k"}), "zai-glm-5-3")
	for _, effort := range []string{"low", "max"} {
		opts := &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "x"},
			ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"reasoningEffort": effort}},
		}
		if err := validateChatProviderOptions(opts.ProviderOptions); err != nil {
			t.Fatalf("%s rejected: %v", effort, err)
		}
		if body := m.buildRequestBody(opts, false); body["reasoning_effort"] != effort {
			t.Errorf("provider option %s: reasoning_effort = %v", effort, body["reasoning_effort"])
		}
	}

	// glm-5-2 keeps the existing mapping: none stays "none", no warning.
	none := types.ReasoningNone
	for _, id := range []string{"zai-glm-5-2", "glm-5-2"} {
		lm := NewLanguageModel(New(Config{APIKey: "k"}), id)
		opts := &provider.GenerateOptions{Reasoning: &none, Prompt: types.Prompt{Text: "x"}}
		if body := lm.buildRequestBody(opts, false); body["reasoning_effort"] != "none" {
			t.Errorf("%s: reasoning_effort = %v, want none", id, body["reasoning_effort"])
		}
		if ws := lm.checkReasoningWarnings(opts); len(ws) != 0 {
			t.Errorf("%s: unexpected warnings %v", id, ws)
		}
	}
}
