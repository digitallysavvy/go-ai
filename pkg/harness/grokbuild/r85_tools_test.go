package grokbuild

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
)

// Ports TS grok-build-harness.test.ts describe('resolveHostToolCall')
// (03dac28304).
func TestR85toolsResolveHostToolCall(t *testing.T) {
	opts := func(raw any) acp.ResolveHostToolCallOptions {
		return acp.ResolveHostToolCallOptions{ToolCall: acp.ToolCall{ToolCallID: "call-1", Title: "use_tool", RawInput: raw}, ServerName: "ai-sdk-harness-tools", ToolNames: []string{"weather"}}
	}
	for _, raw := range []map[string]any{
		{"tool_name": "ai-sdk-harness-tools__weather", "tool_input": map[string]any{"city": "Paris"}},
		{"variant": "UseTool", "tool_name": "ai-sdk-harness-tools__weather", "tool_input": map[string]any{"city": "Paris"}},
	} {
		got := ResolveHostToolCall(opts(raw))
		if got == nil || got.ToolName != "weather" || !reflect.DeepEqual(got.Input, map[string]any{"city": "Paris"}) {
			t.Fatalf("ResolveHostToolCall(%v) = %+v", raw, got)
		}
	}
	for _, raw := range []any{
		map[string]any{"query": "weather", "limit": 5},
		map[string]any{"tool_name": "other-server__weather", "tool_input": map[string]any{"city": "Paris"}},
		map[string]any{"tool_name": "ai-sdk-harness-tools__unknown", "tool_input": map[string]any{}},
		map[string]any{"tool_name": "ai-sdk-harness-tools__weather", "tool_input": nil},
	} {
		if got := ResolveHostToolCall(opts(raw)); got != nil {
			t.Errorf("ResolveHostToolCall(%v) = %+v, want nil", raw, got)
		}
	}
}
