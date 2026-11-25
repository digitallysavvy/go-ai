package cursor

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
)

// Ports TS cursor-harness.test.ts describe('resolveHostToolCall')
// (03dac28304) and checks BuildConfig wires the resolver.
func TestR85toolsResolveHostToolCall(t *testing.T) {
	if BuildConfig(Settings{}).ResolveHostToolCall == nil {
		t.Fatal("BuildConfig must set ResolveHostToolCall")
	}
	opts := func(raw any) acp.ResolveHostToolCallOptions {
		return acp.ResolveHostToolCallOptions{ToolCall: acp.ToolCall{ToolCallID: "call-1", Title: "Tool", RawInput: raw}, ServerName: "ai-sdk-harness-tools", ToolNames: []string{"get_weather"}}
	}
	got := ResolveHostToolCall(opts(map[string]any{"providerIdentifier": "ai-sdk-harness-tools", "toolName": "get_weather", "args": map[string]any{"city": "Austin"}}))
	if got == nil || got.ToolName != "get_weather" || !reflect.DeepEqual(got.Input, map[string]any{"city": "Austin"}) {
		t.Fatalf("resolved = %+v", got)
	}
	for _, raw := range []any{
		map[string]any{},
		map[string]any{"providerIdentifier": "other-server", "toolName": "get_weather", "args": map[string]any{"city": "Austin"}},
		map[string]any{"providerIdentifier": "ai-sdk-harness-tools", "toolName": "unknown_tool", "args": map[string]any{"city": "Austin"}},
		map[string]any{"providerIdentifier": "ai-sdk-harness-tools", "toolName": "get_weather", "args": nil},
	} {
		if got := ResolveHostToolCall(opts(raw)); got != nil {
			t.Errorf("ResolveHostToolCall(%v) = %+v, want nil", raw, got)
		}
	}
}

// Ports the TS cursor-harness.test.ts write tool assertion (afac7dee96).
func TestR85toolsWriteBuiltinTool(t *testing.T) {
	w, ok := BuildConfig(Settings{}).BuiltinTools["write"]
	if !ok {
		t.Fatal("no write built-in tool")
	}
	if w.Title != "Write" || w.NativeName != "writeToolCall" || w.ToolUseKind != "edit" {
		t.Fatalf("write = %+v", w)
	}
	want := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}
	if !reflect.DeepEqual(w.Parameters, want) {
		t.Fatalf("write schema = %#v", w.Parameters)
	}
}
