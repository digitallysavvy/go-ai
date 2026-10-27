package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23407: tool-input-start carries inputEncoding, and the UI message
// stream accumulates text-encoded deltas as a raw string instead of parsing
// them as partial JSON.
func TestReadUIMessages_TextInputEncodingAccumulatesRawText(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		start := UIMessageChunk{"type": "tool-input-start", "toolCallId": "c1", "toolName": "setHtml", "inputEncoding": "text"}
		if dynamic {
			start["dynamic"] = true
		}
		snapshots, err := sendChunks(t, nil, []UIMessageChunk{
			{"type": "start"}, {"type": "start-step"},
			start,
			{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": "<div>"},
			{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": "hi</div>"},
			{"type": "tool-input-available", "toolCallId": "c1", "toolName": "setHtml", "input": "<div>hi</div>", "dynamic": dynamic},
			{"type": "finish-step"}, {"type": "finish"},
		})
		require.NoError(t, err)
		toolType := "tool-setHtml"
		if dynamic {
			toolType = "dynamic-tool"
		}
		var streaming []*ToolUIPart
		for _, snap := range snapshots {
			if p := findToolUIMessagePart(snap, toolType); p != nil && p.State == ToolStateInputStreaming && p.RawInput != nil {
				cp := *p
				streaming = append(streaming, &cp)
			}
		}
		require.Len(t, streaming, 2, "dynamic=%v", dynamic)
		assert.Equal(t, "<div>", streaming[0].Input)
		assert.Equal(t, "<div>hi</div>", streaming[1].Input)
		assert.Equal(t, "<div>hi</div>", streaming[1].RawInput)
		assert.Equal(t, "text", streaming[1].InputEncoding)

		final := findToolUIMessagePart(snapshots[len(snapshots)-1], toolType)
		require.NotNil(t, final)
		assert.Equal(t, ToolStateInputAvailable, final.State)
		assert.Equal(t, "<div>hi</div>", final.Input)
		assert.Empty(t, final.InputEncoding, "inputEncoding is cleared once the input is complete")
	}
}

// Without inputEncoding the deltas are still parsed as partial JSON.
func TestReadUIMessages_DefaultInputEncodingParsesPartialJSON(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start"}, {"type": "start-step"},
		{"type": "tool-input-start", "toolCallId": "c1", "toolName": "search"},
		{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": `{"q":"a"}`},
		{"type": "finish-step"}, {"type": "finish"},
	})
	require.NoError(t, err)
	p := findToolUIMessagePart(snapshots[len(snapshots)-1], "tool-search")
	require.NotNil(t, p)
	assert.Equal(t, map[string]interface{}{"q": "a"}, p.Input)
	assert.Empty(t, p.InputEncoding)
}

// A hydrated input-streaming part resumes with its stored encoding.
func TestReadUIMessages_HydratedTextInputEncodingContinues(t *testing.T) {
	msg := &UIMessage{ID: "m1", Role: "assistant", Parts: []UIMessagePart{&ToolUIPart{
		Type: "tool-setHtml", ToolCallID: "c1", State: ToolStateInputStreaming,
		Input: "<div>", RawInput: "<div>", InputEncoding: "text",
	}}}
	snapshots, err := sendChunks(t, msg, []UIMessageChunk{
		{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": "x"},
	})
	require.NoError(t, err)
	p := findToolUIMessagePart(snapshots[len(snapshots)-1], "tool-setHtml")
	require.NotNil(t, p)
	assert.Equal(t, "<div>x", p.Input)
}

func TestToolInputStartInputEncodingReachesUIMessageChunk(t *testing.T) {
	chunks := convertProviderChunkToUIMessageChunks(provider.StreamChunk{
		Type:     provider.ChunkTypeToolInputStart,
		ToolCall: &types.ToolCall{ID: "c1", ToolName: "setHtml", InputEncoding: "text"},
	}, resultChunkConversionOptions{})
	require.Len(t, chunks, 1)
	assert.Equal(t, "text", chunks[0]["inputEncoding"])

	chunks = convertProviderChunkToUIMessageChunks(provider.StreamChunk{
		Type:     provider.ChunkTypeToolInputStart,
		ToolCall: &types.ToolCall{ID: "c1", ToolName: "setHtml"},
	}, resultChunkConversionOptions{})
	require.Len(t, chunks, 1)
	_, has := chunks[0]["inputEncoding"]
	assert.False(t, has)
}

func TestValidateUIMessages_InputEncodingMustBeJSONOrText(t *testing.T) {
	good := UIMessageChunk{"id": "m", "role": "assistant", "parts": []interface{}{map[string]interface{}{
		"type": "tool-setHtml", "toolCallId": "c1", "state": "input-streaming", "inputEncoding": "text",
	}}}
	_, err := UIMessagesFromChunks([]UIMessageChunk{good})
	require.NoError(t, err)
	bad := UIMessageChunk{"id": "m", "role": "assistant", "parts": []interface{}{map[string]interface{}{
		"type": "tool-setHtml", "toolCallId": "c1", "state": "input-streaming", "inputEncoding": "xml",
	}}}
	assert.NotEmpty(t, uiToolPartIssue(bad["parts"].([]interface{})[0].(map[string]interface{})))
}
