package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS dispatches response.output_item.done on the done item's own type: a
// function_call done item with no recorded output_item.added still yields
// tool-input-end and tool-call, and a done type that differs from the added
// type follows the done type.
func TestResponsesStreamDoneEventDispatchesOnDoneItemType(t *testing.T) {
	cases := map[string]string{
		"no added event": `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","status":"completed","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"a\":1}"}}` + "\n\n",
		"added message, done function_call": `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}` + "\n\n" +
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","status":"completed","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"a\":1}"}}` + "\n\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := newResponsesStream(io.NopCloser(strings.NewReader(body)), false)
			var call *provider.StreamChunk
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeToolCall {
					call = c
				}
			}
			if call == nil || call.ToolCall.ID != "call_1" || call.ToolCall.ToolName != "weather" || call.ToolCall.RawArguments != `{"a":1}` {
				t.Fatalf("tool call = %+v", call)
			}
		})
	}
}
