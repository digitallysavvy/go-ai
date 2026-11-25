package ai

import "testing"

// TS builds the UIMessageStreamError messages with template literals: the id
// is quoted as is ("${chunk.id}"), not escaped.
func TestR65UIMessageStreamErrorQuotesIDAsIs(t *testing.T) {
	cases := []struct {
		chunk UIMessageChunk
		want  string
	}{
		{UIMessageChunk{"type": "text-delta", "id": `a"b\c`, "delta": "x"}, `Received text-delta for missing text part with ID "a"b\c". Ensure a "text-start" chunk is sent before any "text-delta" chunks.`},
		{UIMessageChunk{"type": "tool-input-delta", "toolCallId": `a"b`, "inputTextDelta": "x"}, `Received tool-input-delta for missing tool call with ID "a"b". Ensure a "tool-input-start" chunk is sent before any "tool-input-delta" chunks.`},
		{UIMessageChunk{"type": "tool-output-available", "toolCallId": `a"b`, "output": 1}, `No tool invocation found for tool call ID "a"b".`},
		{UIMessageChunk{"type": "tool-approval-response", "approvalId": `a"b`, "approved": true}, `No tool invocation found for approval ID "a"b".`},
	}
	for _, c := range cases {
		err := applyUIStateChunk(newUIMessageCallbackState(nil, "m"), c.chunk, func(error) string { return "" })
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.chunk["type"], err, c.want)
		}
	}
}
