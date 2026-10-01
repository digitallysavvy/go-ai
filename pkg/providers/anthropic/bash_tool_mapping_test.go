package anthropic

import (
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAnthropicBashToolUseMapsProviderNameToSDKToolName(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "test-key"}), "claude-sonnet-4-6", nil)
	result := model.convertResponse(anthropicResponse{
		Content: []anthropicContent{
			{
				Type:  "tool_use",
				ID:    "toolu_1",
				Name:  "bash",
				Input: map[string]interface{}{"command": "echo hi"},
			},
		},
		StopReason: "tool_use",
	}, false, []types.Tool{{Name: "anthropic.bash_20250124"}})

	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want one", result.ToolCalls)
	}
	if result.ToolCalls[0].ToolName != "anthropic.bash_20250124" {
		t.Fatalf("tool name = %q, want anthropic.bash_20250124", result.ToolCalls[0].ToolName)
	}
}

func TestAnthropicBashStreamMapsProviderNameToSDKToolName(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"bash","input":{}}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"echo hi\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
	})
	stream := newAnthropicStream(body, false, []types.Tool{{Name: "anthropic.bash_20250124"}})

	start, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error = %v", err)
	}
	if start.Type != provider.ChunkTypeToolInputStart || start.ToolCall.ToolName != "anthropic.bash_20250124" {
		t.Fatalf("start chunk = %#v, want mapped bash tool-input-start", start)
	}

	var call *types.ToolCall
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream error = %v", err)
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			call = chunk.ToolCall
			break
		}
	}
	if call == nil {
		t.Fatal("missing tool-call chunk")
	}
	if call.ToolName != "anthropic.bash_20250124" {
		t.Fatalf("tool-call name = %q, want anthropic.bash_20250124", call.ToolName)
	}
}
