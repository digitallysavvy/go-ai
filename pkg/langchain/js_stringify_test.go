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

// TS reads tool calls only from AI messages and only skips on a falsy id; the
// call_<index> id fallback belongs to the OpenAI additional_kwargs format.
func TestValuesToolCallsFollowTS(t *testing.T) {
	state := newEventState()
	values := func(msgs ...interface{}) []map[string]interface{} {
		out := []map[string]interface{}{}
		for _, c := range processValuesEvent(state, StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": msgs}}) {
			out = append(out, c)
		}
		return out
	}
	// normalized call without an id: skipped
	got := values(map[string]interface{}{"type": "ai", "id": "m1", "tool_calls": []interface{}{map[string]interface{}{"name": "t", "args": map[string]interface{}{}}}})
	for _, c := range got {
		if c["type"] == "tool-input-start" {
			t.Fatalf("a normalized tool call without id was emitted: %v", got)
		}
	}
	// OpenAI format without id: call_0
	got = values(map[string]interface{}{"type": "ai", "id": "m2", "additional_kwargs": map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"function": map[string]interface{}{"name": "t", "arguments": "{}"}}}}})
	if len(got) < 1 || got[0]["toolCallId"] != "call_0" {
		t.Fatalf("openai tool call = %v", got)
	}
	// non-AI message with tool_calls: ignored
	got = values(map[string]interface{}{"type": "human", "id": "m3", "tool_calls": []interface{}{map[string]interface{}{"id": "x", "name": "t"}}})
	if len(got) != 0 {
		t.Fatalf("human message tool calls emitted: %v", got)
	}
	// nil args: input left off
	got = values(map[string]interface{}{"type": "ai", "id": "m4", "tool_calls": []interface{}{map[string]interface{}{"id": "c4", "name": "t"}}})
	if _, has := got[len(got)-1]["input"]; has {
		t.Fatalf("undefined input was sent: %v", got)
	}
}
