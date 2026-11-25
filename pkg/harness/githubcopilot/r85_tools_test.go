package githubcopilot

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
)

// Ports TS github-copilot-harness.test.ts describe('resolveHostToolCall')
// (03dac28304): a nameless call titled `<server>-<tool>` resolves with its
// raw input; a named call or a non-object input does not.
func TestR85toolsResolveHostToolCall(t *testing.T) {
	opts := func(tc acp.ToolCall) acp.ResolveHostToolCallOptions {
		return acp.ResolveHostToolCallOptions{ToolCall: tc, ServerName: "ai-sdk-harness-tools", ToolNames: []string{"weather"}}
	}
	got := ResolveHostToolCall(opts(acp.ToolCall{ToolCallID: "c", Title: "ai-sdk-harness-tools-weather", RawInput: map[string]any{"city": "Paris"}}))
	if got == nil || got.ToolName != "weather" || !reflect.DeepEqual(got.Input, map[string]any{"city": "Paris"}) {
		t.Fatalf("resolved = %+v", got)
	}
	for _, tc := range []acp.ToolCall{
		{ToolCallID: "c", Name: "weather", Title: "ai-sdk-harness-tools-weather", RawInput: map[string]any{"city": "Paris"}},
		{ToolCallID: "c", Title: "ai-sdk-harness-tools-weather", RawInput: "x"},
		{ToolCallID: "c", Title: "other-weather", RawInput: map[string]any{}},
	} {
		if got := ResolveHostToolCall(opts(tc)); got != nil {
			t.Errorf("ResolveHostToolCall(%+v) = %+v, want nil", tc, got)
		}
	}
}
