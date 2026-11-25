package workflow

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseToolCall keeps the model's input text for an invalid call whose text
// is not JSON, and the tool-input-error chunk carries it.
func TestToUIMessageChunkInvalidToolCallKeepsInputText(t *testing.T) {
	chunk, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
		ID: "c1", ToolName: "t", RawArguments: `{"a":`, Invalid: true, Error: errors.New("bad input"),
	}})
	if !ok || chunk["type"] != "tool-input-error" || chunk["input"] != `{"a":` {
		t.Fatalf("chunk = %#v", chunk)
	}
	chunk, _ = ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
		ID: "c1", ToolName: "t", RawArguments: `{"a":1}`, Arguments: map[string]interface{}{"a": 1.0}, Invalid: true,
	}})
	if m, ok := chunk["input"].(map[string]interface{}); !ok || m["a"] != 1.0 {
		t.Fatalf("parsed input = %#v", chunk["input"])
	}
}
