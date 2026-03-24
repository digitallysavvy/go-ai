package groq

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestGroqStream_TextChunks(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close()

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

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeText || chunks[0].Text != "Hello" {
		t.Errorf("chunk[0]: got type=%v text=%q", chunks[0].Type, chunks[0].Text)
	}
	if chunks[1].Type != provider.ChunkTypeText || chunks[1].Text != " world" {
		t.Errorf("chunk[1]: got type=%v text=%q", chunks[1].Type, chunks[1].Text)
	}
	if chunks[2].Type != provider.ChunkTypeFinish {
		t.Errorf("chunk[2]: expected finish, got %v", chunks[2].Type)
	}
}

// TestGroqStream_ToolCallPartialJSONNotFinalized verifies tool calls are accumulated
// across deltas and only emitted at finish_reason, never based on JSON parsability.
func TestGroqStream_ToolCallPartialJSONNotFinalized(t *testing.T) {
	// The second chunk delivers {"ready":true} which is complete valid JSON.
	// The old (buggy) code would have emitted the tool call immediately.
	// The fix requires waiting until finish_reason in the final chunk.
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fn","arguments":""}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ready\":true}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close()

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

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (tool_call + finish), got %d", len(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk[0]: expected tool_call, got %v", chunks[0].Type)
	}
	if chunks[0].ToolCall.ID != "call_1" {
		t.Errorf("tool call id: got %q", chunks[0].ToolCall.ID)
	}
	if chunks[0].ToolCall.ToolName != "fn" {
		t.Errorf("tool call name: got %q", chunks[0].ToolCall.ToolName)
	}
	if chunks[0].ToolCall.Arguments["ready"] != true {
		t.Errorf("tool call arg ready: got %v", chunks[0].ToolCall.Arguments["ready"])
	}
	if chunks[1].Type != provider.ChunkTypeFinish {
		t.Errorf("chunk[1]: expected finish, got %v", chunks[1].Type)
	}
	if chunks[1].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish reason: got %v", chunks[1].FinishReason)
	}
}

// TestGroqStream_ToolCallFinalizedAtFlush verifies tool calls are emitted only at stream end.
func TestGroqStream_ToolCallFinalizedAtFlush(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"weather","arguments":"{\"city\":"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"NYC\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close()

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

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeToolCall {
		t.Fatalf("expected tool_call chunk, got %v", chunks[0].Type)
	}
	if chunks[0].ToolCall.Arguments["city"] != "NYC" {
		t.Errorf("expected city=NYC, got %v", chunks[0].ToolCall.Arguments["city"])
	}
	if chunks[1].Type != provider.ChunkTypeFinish {
		t.Errorf("expected finish chunk, got %v", chunks[1].Type)
	}
}
