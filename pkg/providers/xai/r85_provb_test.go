package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS xai-model-catalog.ts modelsWithoutNoneReasoningEffort, evaluated by
// Node 24 at 30548ee7f9 (true = "none" is accepted).
func TestR85provbSupportsNoneReasoningEffortMatchesNode(t *testing.T) {
	want := map[string]bool{"grok-4": true, "grok-4.5": false, "grok-4.6": false, "grok-4.7": false, "grok-4.9-fast": false, "grok-4.20": true, "grok-4.20-reasoning": true, "grok-4.55": true, "grok-4.5x": false, "grok-5": false, "grok-5-mini": false, "grok-5.1": false, "grok-50": false, "grok-10": false, "grok-1": true, "grok-3-mini": true, "grok-0": true, "grok-5x": true, "grok-4-1-fast-reasoning": true, "grok-code-fast-1": true, "grok-4.6-2026": false, "grok-09": true}
	for id, w := range want {
		if got := supportsNoneReasoningEffort(id); got != w {
			t.Errorf("supportsNoneReasoningEffort(%q) = %v, want %v", id, got, w)
		}
	}
}

// TS 61a315e333: reasoning "none" on Grok 4.5+ maps to the lowest effort.
func TestR85provbGrokNoneReasoningMapsToLow(t *testing.T) {
	none := types.ReasoningNone
	for id, want := range map[string]string{"grok-4.7": "low", "grok-4": "none"} {
		m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), id)
		body, warnings, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &none}, false)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := body["reasoning"].(map[string]interface{})
		if r["effort"] != want {
			t.Errorf("%s: effort = %v, want %s", id, r["effort"], want)
		}
		if want == "low" && (len(warnings) != 1 || warnings[0].Type != "compatibility" || warnings[0].Details != `reasoning "none" is not directly supported by this model. mapped to effort "low".`) {
			t.Errorf("%s: warnings = %#v", id, warnings)
		}
	}
}
