package streaming

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS finalizes tool calls in flush(), not at finish_reason: arguments that keep
// streaming after a finish_reason chunk belong to the same call.
func TestOpenAICompatStream_ToolCallsFinalizeAtStreamEnd(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{\"a\":"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}` + "\n\n" +
		"data: [DONE]\n\n"
	chunks := collectStreamChunks(t, newTestStream(sse))
	var call *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			call = c
		}
	}
	if call == nil {
		t.Fatalf("no tool call in %v", chunks)
	}
	if v, _ := call.ToolCall.Arguments["a"].(float64); v != 1 {
		t.Fatalf("args = %v", call.ToolCall.Arguments)
	}
	if last := chunks[len(chunks)-1]; last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("last chunk = %+v", last)
	}
}

// An error chunk after finish_reason turns the finish reason into error (TS).
func TestOpenAICompatStream_ErrorAfterFinishReasonIsErrorFinish(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}` + "\n\n" +
		`data: {"error":{"message":"boom"}}` + "\n\n" +
		"data: [DONE]\n\n"
	chunks := collectStreamChunks(t, newTestStream(sse))
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonError {
		t.Fatalf("last chunk = %+v", last)
	}
}
