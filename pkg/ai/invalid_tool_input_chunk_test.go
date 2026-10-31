package ai

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// An invalid tool call whose input text is not JSON reports the text in its
// tool-input-error chunk (TS parseToolCall keeps toolCall.input).
func TestToolInputErrorChunkKeepsUnparsedInputText(t *testing.T) {
	chunks := convertProviderChunkToUIMessageChunks(provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
		ID: "c1", ToolName: "t", RawArguments: `{"a":`, Invalid: true, Error: errors.New("bad"),
	}}, resultChunkConversionOptions{OnError: func(err error) string { return err.Error() }})
	if len(chunks) == 0 || chunks[0]["type"] != "tool-input-error" || chunks[0]["input"] != `{"a":` {
		t.Fatalf("chunks = %#v", chunks)
	}
}
