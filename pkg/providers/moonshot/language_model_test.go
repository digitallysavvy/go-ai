package moonshot

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func moonshotChunksOfType(chunks []*provider.StreamChunk, chunkType provider.ChunkType) []*provider.StreamChunk {
	var filtered []*provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == chunkType {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func TestMoonshotStream_TextChunks(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

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

// TestMoonshotStream_ToolCallPartialJSONNotFinalized verifies that tool calls are
// accumulated across deltas and only emitted at finish_reason.
func TestMoonshotStream_ToolCallPartialJSONNotFinalized(t *testing.T) {
	// Second chunk delivers {"ready":true} — valid JSON mid-stream.
	// The fix requires waiting until finish_reason in the final chunk.
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fn","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ready\":true}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

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

	toolCalls := moonshotChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ID != "call_1" {
		t.Errorf("tool call id: got %q", toolCalls[0].ToolCall.ID)
	}
	if toolCalls[0].ToolCall.ToolName != "fn" {
		t.Errorf("tool call name: got %q", toolCalls[0].ToolCall.ToolName)
	}
	if toolCalls[0].ToolCall.Arguments["ready"] != true {
		t.Errorf("tool call arg ready: got %v", toolCalls[0].ToolCall.Arguments["ready"])
	}
	finishes := moonshotChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
	if finishes[0].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish reason: got %v", finishes[0].FinishReason)
	}
}

// TestMoonshotStream_FinishWithUsage verifies that usage data in the finish
// chunk is captured correctly.
func TestMoonshotStream_FinishWithUsage(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)))
	defer stream.Close() //nolint:errcheck

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
	finish := chunks[1]
	if finish.Type != provider.ChunkTypeFinish {
		t.Fatalf("expected finish chunk, got %v", finish.Type)
	}
	if finish.Usage == nil {
		t.Fatal("expected usage to be set on finish chunk")
	}
	if finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 10 {
		t.Errorf("expected input tokens 10, got %v", finish.Usage.InputTokens)
	}
	if finish.Usage.OutputTokens == nil || *finish.Usage.OutputTokens != 5 {
		t.Errorf("expected output tokens 5, got %v", finish.Usage.OutputTokens)
	}
}
