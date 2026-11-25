package responses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the intent of ai/packages/openai/src/tool/programmatic-tool-calling.ts's
// experimental_toolCaller wiring: prepareProviderOptions appends
// "programmatic" to providerOptions.openai.allowedCallers.
func TestNewProgrammaticToolCallingTool_ToolCaller(t *testing.T) {
	tool := NewProgrammaticToolCallingTool()
	if tool.ExperimentalToolCaller == nil {
		t.Fatal("ExperimentalToolCaller is nil")
	}
	if tool.ExperimentalToolCaller.Type != types.ToolCallerTypeProvider {
		t.Fatalf("Type = %q, want %q", tool.ExperimentalToolCaller.Type, types.ToolCallerTypeProvider)
	}

	t.Run("adds allowedCallers to empty providerOptions", func(t *testing.T) {
		out := tool.ExperimentalToolCaller.PrepareProviderOptions(nil)
		openaiOpts, ok := out["openai"].(map[string]interface{})
		if !ok {
			t.Fatalf("out[openai] = %#v, want map", out["openai"])
		}
		callers, ok := openaiOpts["allowedCallers"].([]string)
		if !ok || len(callers) != 1 || callers[0] != "programmatic" {
			t.Fatalf("allowedCallers = %v", openaiOpts["allowedCallers"])
		}
	})

	t.Run("appends without duplicating and preserves other options", func(t *testing.T) {
		in := map[string]interface{}{
			"openai": map[string]interface{}{
				"allowedCallers": []string{"programmatic", "direct"},
				"other":          "keep",
			},
			"unrelated": "value",
		}
		out := tool.ExperimentalToolCaller.PrepareProviderOptions(in)
		if out["unrelated"] != "value" {
			t.Fatalf("unrelated = %v", out["unrelated"])
		}
		openaiOpts, ok := out["openai"].(map[string]interface{})
		if !ok {
			t.Fatalf("out[openai] = %#v, want map", out["openai"])
		}
		if openaiOpts["other"] != "keep" {
			t.Fatalf("other = %v", openaiOpts["other"])
		}
		callers, ok := openaiOpts["allowedCallers"].([]string)
		if !ok || len(callers) != 2 || callers[0] != "programmatic" || callers[1] != "direct" {
			t.Fatalf("allowedCallers = %v", openaiOpts["allowedCallers"])
		}
	})

	t.Run("keeps existing callers first and appends programmatic (TS Set order)", func(t *testing.T) {
		in := map[string]interface{}{"openai": map[string]interface{}{"allowedCallers": []string{"direct"}}}
		out := tool.ExperimentalToolCaller.PrepareProviderOptions(in)
		callers := out["openai"].(map[string]interface{})["allowedCallers"].([]string)
		if len(callers) != 2 || callers[0] != "direct" || callers[1] != "programmatic" {
			t.Fatalf("allowedCallers = %v, want [direct programmatic]", callers)
		}
	})

	t.Run("supports deferred results like TS", func(t *testing.T) {
		if !tool.SupportsDeferredResults {
			t.Fatal("SupportsDeferredResults must be true")
		}
	})
}
