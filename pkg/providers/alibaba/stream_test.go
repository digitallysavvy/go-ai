package alibaba

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func alibabaChunksOfType(chunks []*provider.StreamChunk, chunkType provider.ChunkType) []*provider.StreamChunk {
	var filtered []*provider.StreamChunk
	for _, chunk := range chunks {
		if chunk.Type == chunkType {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

// TestAlibabaStream_ProcessTextChunks tests basic text chunk processing
func TestAlibabaStream_ProcessTextChunks(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234567890,"model":"qwen-plus","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":""}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234567890,"model":"qwen-plus","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":""}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1234567890,"model":"qwen-plus","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	// Should have: 2 text chunks + 1 finish
	require.Len(t, chunks, 3)

	// First text chunk
	assert.Equal(t, provider.ChunkTypeText, chunks[0].Type)
	assert.Equal(t, "Hello", chunks[0].Text)

	// Second text chunk
	assert.Equal(t, provider.ChunkTypeText, chunks[1].Type)
	assert.Equal(t, " world", chunks[1].Text)

	// Finish chunk
	assert.Equal(t, provider.ChunkTypeFinish, chunks[2].Type)
	assert.Equal(t, types.FinishReasonStop, chunks[2].FinishReason)
}

// TestAlibabaStream_ProcessReasoningChunks tests reasoning content (thinking mode)
func TestAlibabaStream_ProcessReasoningChunks(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"reasoning_content":"Let me think..."},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"reasoning_content":" about this problem"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":"The answer is 42"},"finish_reason":"stop"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	// Should have: reasoning-start + reasoning + reasoning + reasoning-end + text + finish
	require.Len(t, chunks, 6)

	// Reasoning start
	assert.Equal(t, provider.ChunkTypeReasoningStart, chunks[0].Type)

	// First reasoning chunk
	assert.Equal(t, provider.ChunkTypeReasoning, chunks[1].Type)
	assert.Equal(t, "Let me think...", chunks[1].Reasoning)

	// Second reasoning chunk
	assert.Equal(t, provider.ChunkTypeReasoning, chunks[2].Type)
	assert.Equal(t, " about this problem", chunks[2].Reasoning)

	// Reasoning end + text (text arrives with finish, triggering reasoning-end transition)
	assert.Equal(t, provider.ChunkTypeReasoningEnd, chunks[3].Type)

	// Text chunk
	assert.Equal(t, provider.ChunkTypeText, chunks[4].Type)
	assert.Equal(t, "The answer is 42", chunks[4].Text)

	// Finish chunk
	assert.Equal(t, provider.ChunkTypeFinish, chunks[5].Type)
	assert.Equal(t, types.FinishReasonStop, chunks[5].FinishReason)
}

// TestAlibabaStream_ProcessToolCallChunks tests tool call accumulation
func TestAlibabaStream_ProcessToolCallChunks(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\""}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"SF\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	toolCalls := alibabaChunksOfType(chunks, provider.ChunkTypeToolCall)
	require.Len(t, toolCalls, 1)

	// Tool call chunk
	assert.NotNil(t, toolCalls[0].ToolCall)
	assert.Equal(t, "call_1", toolCalls[0].ToolCall.ID)
	assert.Equal(t, "get_weather", toolCalls[0].ToolCall.ToolName)
	assert.Equal(t, "SF", toolCalls[0].ToolCall.Arguments["location"])

	// Finish chunk
	finishes := alibabaChunksOfType(chunks, provider.ChunkTypeFinish)
	require.Len(t, finishes, 1)
	assert.Equal(t, types.FinishReasonToolCalls, finishes[0].FinishReason)
}

// TestAlibabaStream_ProcessToolCallMultiple tests multiple tool calls
func TestAlibabaStream_ProcessToolCallMultiple(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"SF\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"get_time","arguments":"{\"timezone\":\"PST\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	toolCalls := alibabaChunksOfType(chunks, provider.ChunkTypeToolCall)
	require.Len(t, toolCalls, 2)

	// First tool call
	assert.Equal(t, "call_1", toolCalls[0].ToolCall.ID)
	assert.Equal(t, "get_weather", toolCalls[0].ToolCall.ToolName)

	// Second tool call
	assert.Equal(t, "call_2", toolCalls[1].ToolCall.ID)
	assert.Equal(t, "get_time", toolCalls[1].ToolCall.ToolName)

	// Finish chunk
	finishes := alibabaChunksOfType(chunks, provider.ChunkTypeFinish)
	require.Len(t, finishes, 1)
	assert.Equal(t, types.FinishReasonToolCalls, finishes[0].FinishReason)
}

// TestAlibabaStream_WithUsage tests usage tracking in final chunk
func TestAlibabaStream_WithUsage(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	// Should have: text + finish
	require.Len(t, chunks, 2)

	// Text chunk
	assert.Equal(t, provider.ChunkTypeText, chunks[0].Type)
	assert.Equal(t, "Hello", chunks[0].Text)

	// Finish chunk with usage
	assert.Equal(t, provider.ChunkTypeFinish, chunks[1].Type)
	assert.NotNil(t, chunks[1].Usage)
	assert.Equal(t, int64(10), *chunks[1].Usage.InputTokens)
	assert.Equal(t, int64(5), *chunks[1].Usage.OutputTokens)
	assert.Equal(t, int64(15), *chunks[1].Usage.TotalTokens)
}

// TestAlibabaStream_EmptyChunks tests handling of empty chunks
func TestAlibabaStream_EmptyChunks(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	// Should skip empty chunks and have: text + finish
	require.Len(t, chunks, 2)

	assert.Equal(t, provider.ChunkTypeText, chunks[0].Type)
	assert.Equal(t, "Hello", chunks[0].Text)

	assert.Equal(t, provider.ChunkTypeFinish, chunks[1].Type)
}

// TestAlibabaStream_MalformedJSON tests handling of malformed JSON
func TestAlibabaStream_MalformedJSON(t *testing.T) {
	sseData := `data: {malformed json}

data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	// Should skip malformed chunk and have: text + finish
	require.Len(t, chunks, 2)

	assert.Equal(t, provider.ChunkTypeText, chunks[0].Type)
	assert.Equal(t, "Hello", chunks[0].Text)

	assert.Equal(t, provider.ChunkTypeFinish, chunks[1].Type)
}

// TestAlibabaStream_ToolCallPartialJSONNotFinalized verifies that a tool call whose
// accumulated arguments happen to form valid JSON mid-stream is NOT emitted early.
// This is a regression test for the security fix: tool calls must only be finalized
// at flush time (when finish_reason is received), never based on JSON parsability.
func TestAlibabaStream_ToolCallPartialJSONNotFinalized(t *testing.T) {
	// Chunk 2 delivers {"done":true} which IS valid, parseable JSON by itself.
	// The old (buggy) code would have emitted the tool call at this point.
	// The fix requires it to wait until finish_reason in chunk 3.
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"fn"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"done\":true}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	toolCalls := alibabaChunksOfType(chunks, provider.ChunkTypeToolCall)
	require.Len(t, toolCalls, 1)

	assert.Equal(t, "call_1", toolCalls[0].ToolCall.ID)
	assert.Equal(t, "fn", toolCalls[0].ToolCall.ToolName)
	assert.Equal(t, true, toolCalls[0].ToolCall.Arguments["done"])

	finishes := alibabaChunksOfType(chunks, provider.ChunkTypeFinish)
	require.Len(t, finishes, 1)
	assert.Equal(t, types.FinishReasonToolCalls, finishes[0].FinishReason)
}

// TestAlibabaStream_ToolCallFinalizedAtFlush verifies that tool call chunks are
// only emitted after finish_reason, not before.
func TestAlibabaStream_ToolCallFinalizedAtFlush(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"get_data","arguments":"{\"key\":"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"value\"}"}}]},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	toolCalls := alibabaChunksOfType(chunks, provider.ChunkTypeToolCall)
	require.Len(t, toolCalls, 1)

	assert.Equal(t, "call_x", toolCalls[0].ToolCall.ID)
	assert.Equal(t, "get_data", toolCalls[0].ToolCall.ToolName)
	assert.Equal(t, "value", toolCalls[0].ToolCall.Arguments["key"])

	finishes := alibabaChunksOfType(chunks, provider.ChunkTypeFinish)
	require.Len(t, finishes, 1)
}

// TestAlibabaStream_MixedContent tests mixed text and reasoning content
func TestAlibabaStream_MixedContent(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{"reasoning_content":"Thinking..."},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":"Answer: "},"finish_reason":""}]}

data: {"choices":[{"index":0,"delta":{"content":"42"},"finish_reason":"stop"}]}

data: [DONE]

`

	reader := io.NopCloser(strings.NewReader(sseData))
	stream := newAlibabaStream(reader)
	defer stream.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	// Should have: reasoning-start + reasoning + reasoning-end + 2 text + finish
	require.Len(t, chunks, 6)

	assert.Equal(t, provider.ChunkTypeReasoningStart, chunks[0].Type)

	assert.Equal(t, provider.ChunkTypeReasoning, chunks[1].Type)
	assert.Equal(t, "Thinking...", chunks[1].Reasoning)

	assert.Equal(t, provider.ChunkTypeReasoningEnd, chunks[2].Type)

	assert.Equal(t, provider.ChunkTypeText, chunks[3].Type)
	assert.Equal(t, "Answer: ", chunks[3].Text)

	assert.Equal(t, provider.ChunkTypeText, chunks[4].Type)
	assert.Equal(t, "42", chunks[4].Text)

	assert.Equal(t, provider.ChunkTypeFinish, chunks[5].Type)
}
