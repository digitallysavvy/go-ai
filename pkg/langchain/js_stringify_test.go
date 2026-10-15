package langchain

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS JSON.stringify does not escape <, > or &; Go's default encoder does.
func TestToolTextUsesJSStringifyEscaping(t *testing.T) {
	got := formatToolError(map[string]interface{}{"msg": "a<b>&c"})
	if got != `{"msg":"a<b>&c"}` {
		t.Fatalf("formatToolError = %s", got)
	}
	state := newEventState()
	chunks := processToolsEvent(state, StreamEvent{Mode: "tools", Data: map[string]interface{}{"event": "on_tool_error", "toolCallId": "c1", "name": "t"}})
	last := chunks[len(chunks)-1]
	if last["errorText"] != "undefined" {
		t.Fatalf("missing error text = %v, want undefined", last["errorText"])
	}
	msg := convertToolResultPart(types.ToolResultContent{ToolCallID: "c1", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"x": "<&>"}}})
	if msg["content"] != `{"x":"<&>"}` {
		t.Fatalf("tool message content = %v", msg["content"])
	}
}
