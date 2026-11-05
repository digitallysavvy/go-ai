package openresponses

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r87eprovbStreamToolInput runs SSE events through the stream and returns
// the input of the first tool call.
func r87eprovbStreamToolInput(t *testing.T, events ...string) string {
	t.Helper()
	var sse strings.Builder
	for _, e := range events {
		sse.WriteString("data: " + e + "\n\n")
	}
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(sse.String())), nil)
	for {
		chunk, err := s.Next()
		if err != nil {
			t.Fatalf("no tool call: %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			return chunk.ToolCall.RawArguments
		}
	}
}

// TS doStream keys toolCallsByItemId / customToolCallsByItemId (a Map) by
// the raw chunk.item.id / chunk.item_id, so a numeric id 5 and a string
// "5" are different keys (SameValueZero), as are null and undefined.
// Traced on TS 3fab50aa9b open-responses-language-model.ts: the delta for
// "5" misses the entry set for 5 and opens its own; the done event for 5
// finds the original entry, whose arguments are "", so the input is "".
func TestR87eprovbStreamItemIDMapKeysKeepJSONType(t *testing.T) {
	added := func(id string) string {
		return `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":` + id + `,"call_id":"c1","name":"a","arguments":""}}`
	}
	delta := func(id string) string {
		return `{"type":"response.function_call_arguments.delta","output_index":0,"item_id":` + id + `,"delta":"{\"x\":1}"}`
	}
	done := func(id string) string {
		return `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":` + id + `,"call_id":"c1","name":"a","arguments":""}}`
	}
	for _, tc := range []struct {
		name, itemID, deltaID, want string
	}{
		{"number item, string delta", `5`, `"5"`, ``},
		{"string item, number delta", `"5"`, `5`, ``},
		{"number item, number delta", `5`, `5.0`, `{"x":1}`},
		{"string ids", `"5"`, `"5"`, `{"x":1}`},
		{"null item, absent-like string", `null`, `"null"`, ``},
		{"true item, string delta", `true`, `"true"`, ``},
		{"object ids never match", `{}`, `{}`, ``},
	} {
		if got := r87eprovbStreamToolInput(t, added(tc.itemID), delta(tc.deltaID), done(tc.itemID)); got != tc.want {
			t.Errorf("%s: input = %q, want %q", tc.name, got, tc.want)
		}
	}

	// Custom tool calls use the same Map semantics.
	customAdded := `{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":7,"call_id":"k1","name":"t","input":""}}`
	customDelta := `{"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"7","delta":"abc"}`
	customDone := `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":7,"call_id":"k1","name":"t","input":""}}`
	same := `{"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":7,"delta":"abc"}`
	// TS input: JSON.stringify(item.input !== '' ? item.input : toolCall.input).
	if got := r87eprovbStreamToolInput(t, customAdded, customDelta, customDone); got != `""` {
		t.Errorf("custom number vs string: input = %q, want %q", got, `""`)
	}
	if got := r87eprovbStreamToolInput(t, customAdded, same, customDone); got != `"abc"` {
		t.Errorf("custom same key: input = %q, want %q", got, `"abc"`)
	}
}
