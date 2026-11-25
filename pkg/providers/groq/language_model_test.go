package groq

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func groqChunksOfType(chunks []*provider.StreamChunk, chunkType provider.ChunkType) []*provider.StreamChunk {
	var filtered []*provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == chunkType {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func TestGroqStream_TextChunks(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sseData)))
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

	// TS emits response-metadata on the first valid chunk, even without
	// id/model/created, then text-start / text-delta / text-end with id
	// "txt-0" (groq-chat-language-model.ts; cr-r87e-stream-to-prova 1).
	if len(chunks) != 6 || chunks[0].Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("expected response-metadata + 5 chunks, got %d", len(chunks))
	}
	chunks = chunks[1:]
	want := []struct {
		typ  provider.ChunkType
		id   string
		text string
	}{
		{provider.ChunkTypeTextStart, "txt-0", ""},
		{provider.ChunkTypeText, "txt-0", "Hello"},
		{provider.ChunkTypeText, "txt-0", " world"},
		{provider.ChunkTypeTextEnd, "txt-0", ""},
		{provider.ChunkTypeFinish, "", ""},
	}
	for i, w := range want {
		if chunks[i].Type != w.typ || chunks[i].ID != w.id || chunks[i].Text != w.text {
			t.Errorf("chunk[%d]: got type=%v id=%q text=%q, want %v %q %q", i, chunks[i].Type, chunks[i].ID, chunks[i].Text, w.typ, w.id, w.text)
		}
	}
}

func TestGroqStream_IncludeRawChunksMatchesTypeScript(t *testing.T) {
	sseData := `data: {"id":"raw-1","created":1702657020,"model":"llama-3.1-8b-instant","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"id":"raw-2","created":1702657020,"model":"llama-3.1-8b-instant","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sseData)), true)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}
	raw, ok := chunk.Raw.(map[string]interface{})
	if !ok || raw["id"] != "raw-1" {
		t.Fatalf("raw chunk = %#v, want id raw-1", chunk.Raw)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("second chunk type = %v, want response-metadata", chunk.Type)
	}
	if chunk.ResponseMetadata == nil || chunk.ResponseMetadata.ID != "raw-1" || chunk.ResponseMetadata.ModelID != "llama-3.1-8b-instant" {
		t.Fatalf("response metadata = %#v, want first provider event metadata", chunk.ResponseMetadata)
	}
	chunk, err = stream.Next()
	if err != nil || chunk.Type != provider.ChunkTypeTextStart {
		t.Fatalf("third chunk = %#v, %v, want text-start", chunk, err)
	}
	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("fourth chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
		t.Fatalf("third chunk = %#v, want text Hello", chunk)
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

	toolCalls := groqChunksOfType(chunks, provider.ChunkTypeToolCall)
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
	finishes := groqChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
	if finishes[0].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish reason: got %v", finishes[0].FinishReason)
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

	toolCalls := groqChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.Arguments["city"] != "NYC" {
		t.Errorf("expected city=NYC, got %v", toolCalls[0].ToolCall.Arguments["city"])
	}
	if finishes := groqChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
}
