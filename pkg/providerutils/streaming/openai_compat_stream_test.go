package streaming

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mapTestFinishReason is the standard mapper used in these tests.
func mapTestFinishReason(reason string) types.FinishReason {
	switch reason {
	case "stop":
		return types.FinishReasonStop
	case "tool_calls":
		return types.FinishReasonToolCalls
	case "length":
		return types.FinishReasonLength
	default:
		return types.FinishReasonOther
	}
}

func newTestStream(sseData string) *OpenAICompatStream {
	return NewOpenAICompatStream(io.NopCloser(strings.NewReader(sseData)), mapTestFinishReason)
}

func collectStreamChunks(t *testing.T, stream provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func compatChunksOfType(chunks []*provider.StreamChunk, chunkType provider.ChunkType) []*provider.StreamChunk {
	var filtered []*provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == chunkType {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func TestOpenAICompatStream_TextChunks(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeText || chunks[0].Text != "Hello" {
		t.Errorf("chunk[0]: got type=%v text=%q", chunks[0].Type, chunks[0].Text)
	}
	if chunks[1].Type != provider.ChunkTypeText || chunks[1].Text != " world" {
		t.Errorf("chunk[1]: got type=%v text=%q", chunks[1].Type, chunks[1].Text)
	}
	if chunks[2].Type != provider.ChunkTypeFinish || chunks[2].FinishReason != types.FinishReasonStop {
		t.Errorf("chunk[2]: expected finish/stop, got type=%v reason=%v", chunks[2].Type, chunks[2].FinishReason)
	}
}

// TestOpenAICompatStream_ToolCallAccumulation verifies that tool call arguments
// are accumulated across deltas and emitted only at finish_reason, not per-delta.
func TestOpenAICompatStream_ToolCallAccumulation(t *testing.T) {
	// Three deltas: first carries id+name+empty args, second carries partial args
	// {"ready":true} (valid JSON mid-stream), third carries finish_reason.
	// The fix requires that no tool call is emitted until the finish_reason delta.
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fn","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ready\":true}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)

	wantTypes := []provider.ChunkType{
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputDelta,
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("expected %d chunks, got %d", len(wantTypes), len(chunks))
	}
	for i, wantType := range wantTypes {
		if chunks[i].Type != wantType {
			t.Fatalf("chunk[%d]: expected %s, got %s", i, wantType, chunks[i].Type)
		}
	}
	if chunks[0].ToolCall.ID != "call_1" || chunks[0].ToolCall.ToolName != "fn" {
		t.Errorf("tool input start: got %#v", chunks[0].ToolCall)
	}
	if chunks[1].Text != `{"ready":true}` {
		t.Errorf("tool input delta: got %q", chunks[1].Text)
	}
	toolCall := chunks[3]
	if toolCall.ToolCall.ID != "call_1" {
		t.Errorf("tool call id: got %q", toolCall.ToolCall.ID)
	}
	if toolCall.ToolCall.ToolName != "fn" {
		t.Errorf("tool call name: got %q", toolCall.ToolCall.ToolName)
	}
	if toolCall.ToolCall.Arguments["ready"] != true {
		t.Errorf("tool call arg ready: got %v", toolCall.ToolCall.Arguments["ready"])
	}
	if chunks[4].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish reason: got %v", chunks[4].FinishReason)
	}
}

// TestOpenAICompatStream_ToolCallPartialJSON verifies that even partial JSON
// arguments that would fail to unmarshal are handled gracefully (nil args).
func TestOpenAICompatStream_ToolCallPartialJSON(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"calc","arguments":"{\"op\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"add\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)

	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.Arguments["op"] != "add" {
		t.Errorf("expected op=add, got %v", toolCalls[0].ToolCall.Arguments["op"])
	}
	if finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
}

// TestOpenAICompatStream_MultipleToolCalls verifies that multiple tool calls
// (different indices) are all flushed correctly at finish_reason.
func TestOpenAICompatStream_MultipleToolCalls(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","type":"function","function":{"name":"f0","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c1","type":"function","function":{"name":"f1","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":2}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)

	// Expect: tool_call[0], tool_call[1], finish
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool-call chunks, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ID != "c0" {
		t.Errorf("toolCalls[0]: expected c0, got %v", toolCalls[0].ToolCall)
	}
	if toolCalls[1].ToolCall.ID != "c1" {
		t.Errorf("toolCalls[1]: expected c1, got %v", toolCalls[1].ToolCall)
	}
	if finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
}

func TestOpenAICompatStream_BuffersArgumentsUntilNameArrives(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"arguments":"{\"city\":\""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"weather","arguments":"Paris\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ToolName != "weather" {
		t.Fatalf("tool name = %q, want weather", toolCalls[0].ToolCall.ToolName)
	}
	if got := toolCalls[0].ToolCall.Arguments["city"]; got != "Paris" {
		t.Fatalf("city = %#v, want Paris", got)
	}
}

func TestOpenAICompatStream_UsesIDFallbackWhenIndexMissing(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"id":"call_1","function":{"arguments":"docs\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ID != "call_1" {
		t.Fatalf("tool call id = %q, want call_1", toolCalls[0].ToolCall.ID)
	}
	if got := toolCalls[0].ToolCall.Arguments["q"]; got != "docs" {
		t.Fatalf("q = %#v, want docs", got)
	}
}

func TestOpenAICompatStream_PreservesToolCallProviderMetadata(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fn","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig123"}}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	meta := toolCalls[0].ToolCall.ProviderMetadata
	google, ok := meta["google"].(map[string]interface{})
	if !ok || google["thoughtSignature"] != "sig123" {
		t.Fatalf("unexpected provider metadata: %#v", meta)
	}
}
