package deepseek

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22045: max maps to DeepSeek's native "max" effort; TS 61a315e333
// rounds the other levels down onto {low, high, max} (medium -> low,
// xhigh -> high, minimal -> low up).
func TestDeepSeekTopLevelReasoningMax(t *testing.T) {
	compat := func(from, to string) []types.Warning {
		d := `reasoning "` + from + `" is not directly supported by this model. mapped to effort "` + to + `".`
		return []types.Warning{{Type: "compatibility", Feature: "reasoning", Details: d, Message: d}}
	}
	for level, want := range map[types.ReasoningLevel]struct {
		effort   string
		warnings []types.Warning
	}{
		types.ReasoningMax:     {"max", nil},
		types.ReasoningHigh:    {"high", nil},
		types.ReasoningLow:     {"low", nil},
		types.ReasoningXHigh:   {"high", compat("xhigh", "high")},
		types.ReasoningMedium:  {"low", compat("medium", "low")},
		types.ReasoningMinimal: {"low", compat("minimal", "low")},
	} {
		level := level
		m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-chat")
		body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &level}, false)
		if err != nil {
			t.Fatal(err)
		}
		if body["reasoning_effort"] != want.effort {
			t.Errorf("%s: reasoning_effort = %v, want %s", level, body["reasoning_effort"], want.effort)
		}
		if !reflect.DeepEqual(warnings, want.warnings) {
			t.Errorf("%s: warnings = %#v", level, warnings)
		}
	}
}
