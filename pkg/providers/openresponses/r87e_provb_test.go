package openresponses

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TS convertToOpenResponsesInput reads `typeof providerData.itemId ===
// 'string'` (and reasoningEncryptedContent the same way) and writes
// `...(itemId != null && {id: itemId})`, so a replayed "" id or encrypted
// content is sent; consecutive reasoning parts with the same "" id merge;
// assistantMessageId undefined and "" are different messages.
func TestR87eprovbReplayKeepsEmptyItemIDs(t *testing.T) {
	opts := func(m map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"open-responses": m}
	}
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ReasoningContent{Text: "a", ProviderOptions: opts(map[string]interface{}{"itemId": "", "reasoningEncryptedContent": ""})},
		types.ReasoningContent{Text: "b", ProviderOptions: opts(map[string]interface{}{"itemId": ""})},
		types.TextContent{Text: "no id"},
		types.TextContent{Text: "empty id", ProviderOptions: opts(map[string]interface{}{"itemId": ""})},
		types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: `{}`, ProviderOptions: opts(map[string]interface{}{"itemId": ""})},
	}}}
	input, _, _, err := ConvertToOpenResponsesInputForProvider(msgs, "", "open-responses")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("items = %s, want reasoning, two messages, function_call", raw)
	}
	r := got[0]
	if id, ok := r["id"]; !ok || id != "" || r["encrypted_content"] != "" {
		t.Fatalf("reasoning = %v, want id \"\" and encrypted_content \"\"", r)
	}
	if content, _ := r["content"].([]interface{}); len(content) != 2 {
		t.Fatalf("reasoning parts with the same \"\" id were not merged: %v", r)
	}
	if _, ok := got[1]["id"]; ok {
		t.Fatalf("message without itemId has an id: %v", got[1])
	}
	if id, ok := got[2]["id"]; !ok || id != "" {
		t.Fatalf("message with itemId \"\" = %v, want id \"\"", got[2])
	}
	if id, ok := got[3]["id"]; !ok || id != "" || got[3]["type"] != "function_call" {
		t.Fatalf("function_call = %v, want id \"\"", got[3])
	}

	// Strict input: an "" message id is a message item with id "", not the
	// plain-string shortcut TS uses only for an undefined id.
	strict, _, _, err := ConvertToOpenResponsesInputForProviderStrict([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.TextContent{Text: "x", ProviderOptions: opts(map[string]interface{}{"itemId": ""})},
	}}}, "", "open-responses", true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(strict)
	if !strings.Contains(string(raw), `"id":""`) || !strings.Contains(string(raw), `"status":"completed"`) {
		t.Fatalf("strict = %s, want a completed message with id \"\"", raw)
	}
}
