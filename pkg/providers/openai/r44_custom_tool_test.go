package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS decodes a custom_tool_call as `input: JSON.stringify(part.input)`; the
// call replays into the next request as the raw text.
func TestR44_CustomToolCallRawArgumentsAndReplay(t *testing.T) {
	stream := newResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"format_code","input":""}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"format_code","input":"print(\"1\")\n","status":"completed"}}

`)), false)
	defer stream.Close() //nolint:errcheck
	var call *types.ToolCall
	for {
		c, err := stream.Next()
		if err != nil || c == nil {
			break
		}
		if c.Type == provider.ChunkTypeToolCall {
			call = c.ToolCall
		}
	}
	if call == nil {
		t.Fatal("no tool call")
	}
	if call.RawArguments != `"print(\"1\")\n"` {
		t.Fatalf("RawArguments = %q", call.RawArguments)
	}
	content := types.ToolCallContent{
		ToolCallID: call.ID, ToolName: call.ToolName, Input: call.RawArguments, Arguments: call.Arguments,
	}
	input, _, err := responses.ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{{
		Role: types.RoleAssistant, Content: []types.ContentPart{content},
	}}}, "system", responses.ConvertOptions{CustomToolNames: map[string]bool{"format_code": true}})
	if err != nil || len(input) != 1 {
		t.Fatalf("input=%v err=%v", input, err)
	}
	item, ok := input[0].(responses.CustomToolCallItem)
	if !ok || item.Input != "print(\"1\")\n" {
		t.Fatalf("replayed item = %#v", input[0])
	}
}
