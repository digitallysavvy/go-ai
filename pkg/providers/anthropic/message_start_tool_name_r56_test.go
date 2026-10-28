package anthropic

import (
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r56: tool_use blocks pre-populated in message_start.message.content map
// their provider tool names to SDK tool names, like streamed tool_use blocks.
func TestAnthropicMessageStartToolUseMapsProviderNameR56(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"echo hi"}}],"usage":{"input_tokens":3}}}`,
		},
		{event: "message_stop", data: `{"type":"message_stop"}`},
	})
	stream := newAnthropicStream(body, false, []types.Tool{{Name: "anthropic.bash_20250124"}})

	var start, call *types.ToolCall
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream error = %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeToolInputStart:
			start = chunk.ToolCall
		case provider.ChunkTypeToolCall:
			call = chunk.ToolCall
		}
	}
	if start == nil || start.ToolName != "anthropic.bash_20250124" {
		t.Fatalf("tool-input-start = %#v, want mapped name anthropic.bash_20250124", start)
	}
	if call == nil || call.ToolName != "anthropic.bash_20250124" {
		t.Fatalf("tool-call = %#v, want mapped name anthropic.bash_20250124", call)
	}
	if call.Arguments["command"] != "echo hi" {
		t.Fatalf("arguments = %#v", call.Arguments)
	}
}
