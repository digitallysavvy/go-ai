package xai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS emits the done item's own `arguments` as the tool-call input, not the
// accumulated deltas.
func TestXAIResponsesStreamToolCallInputIsDoneItemArguments(t *testing.T) {
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"","status":"in_progress"}}

data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"a\":1}"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"f","arguments":"","status":"completed"}}

`)))
	for {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatal("no tool call")
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			if chunk.ToolCall.RawArguments != "" {
				t.Fatalf("RawArguments = %q, want the done item's empty arguments", chunk.ToolCall.RawArguments)
			}
			return
		}
	}
}
