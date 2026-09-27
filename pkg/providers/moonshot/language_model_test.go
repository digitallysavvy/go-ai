package moonshot

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TestMoonshotConvertResponse_ProviderMetadataKeyIsMoonshotAI guards TS
// parity of the providerMetadata output shape: moonshotai-chat-language-
// model.ts's providerOptionsName is "moonshotai" (config.provider.split('.')[0]
// on "moonshotai.chat"), and every test in moonshotai-chat-language-model.test.ts
// reads providerMetadata.moonshotai — not "moonshot", this Go SDK's own
// package/provider-name convention.
func TestMoonshotConvertResponse_ProviderMetadataKeyIsMoonshotAI(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	idx := 0
	resp := moonshotResponse{
		Object: "chat.completion",
		Choices: []moonshotChoice{
			{Index: &idx, Message: moonshotMessage{Role: "assistant", Content: "hi"}, FinishReason: "stop"},
		},
		Usage: []byte(`{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`),
	}
	result, err := model.convertResponse(resp)
	if err != nil {
		t.Fatalf("convertResponse error = %v", err)
	}
	if _, ok := result.ProviderMetadata["moonshotai"]; !ok {
		t.Fatalf("ProviderMetadata = %#v, want a \"moonshotai\" key", result.ProviderMetadata)
	}
	if _, ok := result.ProviderMetadata["moonshot"]; ok {
		t.Fatalf("ProviderMetadata unexpectedly has a \"moonshot\" key: %#v", result.ProviderMetadata)
	}
}

func TestMoonshotStream_TextChunks(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck

	chunks := drainMoonshotStream(t, stream)

	// text-start, "Hello", " world", text-end, finish
	textChunks := moonshotChunksOfType(chunks, provider.ChunkTypeText)
	if len(textChunks) != 2 || textChunks[0].Text != "Hello" || textChunks[1].Text != " world" {
		t.Fatalf("unexpected text chunks: %#v", textChunks)
	}
	if len(moonshotChunksOfType(chunks, provider.ChunkTypeTextStart)) != 1 {
		t.Fatalf("expected exactly 1 text-start chunk, got %#v", chunks)
	}
	if len(moonshotChunksOfType(chunks, provider.ChunkTypeTextEnd)) != 1 {
		t.Fatalf("expected exactly 1 text-end chunk, got %#v", chunks)
	}
	finishes := moonshotChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
}

// drainMoonshotStream reads every chunk from a moonshotStream until EOF,
// failing the test on any other error.
func drainMoonshotStream(t *testing.T, stream *moonshotStream) []*provider.StreamChunk {
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
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck

	chunks := drainMoonshotStream(t, stream)

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
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck

	chunks := drainMoonshotStream(t, stream)

	finishes := moonshotChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d (%#v)", len(finishes), chunks)
	}
	finish := finishes[0]
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

// TestMoonshotStream_ReasoningLifecycle verifies reasoning-start/delta/end
// chunks are emitted before text, and that reasoning ends when text starts.
func TestMoonshotStream_ReasoningLifecycle(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"role":"assistant","reasoning_content":"Thinking "}}]}

data: {"choices":[{"delta":{"reasoning_content":"more..."}}]}

data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck
	chunks := drainMoonshotStream(t, stream)

	starts := moonshotChunksOfType(chunks, provider.ChunkTypeReasoningStart)
	deltas := moonshotChunksOfType(chunks, provider.ChunkTypeReasoning)
	ends := moonshotChunksOfType(chunks, provider.ChunkTypeReasoningEnd)
	if len(starts) != 1 || len(ends) != 1 {
		t.Fatalf("expected exactly 1 reasoning-start and 1 reasoning-end, got starts=%d ends=%d", len(starts), len(ends))
	}
	if len(deltas) != 2 || deltas[0].Reasoning != "Thinking " || deltas[1].Reasoning != "more..." {
		t.Fatalf("unexpected reasoning deltas: %#v", deltas)
	}
}

// TestMoonshotStream_EmptyReasoningDoesNotStartBlock verifies an empty-string
// reasoning_content delta (falsy in JS/TS) does not open a reasoning block.
func TestMoonshotStream_EmptyReasoningDoesNotStartBlock(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"delta":{"reasoning_content":""}}]}

data: {"choices":[{"delta":{"content":"OK"},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck
	chunks := drainMoonshotStream(t, stream)

	if len(moonshotChunksOfType(chunks, provider.ChunkTypeReasoningStart)) != 0 {
		t.Fatalf("expected no reasoning-start chunk, got %#v", chunks)
	}
}

// TestMoonshotStream_IndexlessToolCalls verifies tool call deltas without an
// explicit "index" field are disambiguated by their position within each
// delta's tool_calls array, mirroring TS's `toolCallDelta.index ?? index`.
func TestMoonshotStream_IndexlessToolCalls(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"id":"weather_0","type":"function","function":{"name":"weather","arguments":""}},{"id":"time_1","type":"function","function":{"name":"time","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"location\":\"San Francisco\"}"}},{"function":{"arguments":"{\"zone\":\"UTC\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck
	chunks := drainMoonshotStream(t, stream)

	if len(moonshotChunksOfType(chunks, provider.ChunkTypeError)) != 0 {
		t.Fatalf("expected no error chunks, got %#v", chunks)
	}
	toolCalls := moonshotChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool-call chunks, got %d (%#v)", len(toolCalls), toolCalls)
	}
	if toolCalls[0].ToolCall.ID != "weather_0" || toolCalls[0].ToolCall.ToolName != "weather" {
		t.Errorf("unexpected first tool call: %#v", toolCalls[0].ToolCall)
	}
	if toolCalls[1].ToolCall.ID != "time_1" || toolCalls[1].ToolCall.ToolName != "time" {
		t.Errorf("unexpected second tool call: %#v", toolCalls[1].ToolCall)
	}
}

// TestMoonshotStream_MalformedToolCallIndex verifies a non-numeric "index"
// field produces an error chunk instead of a panic or silent drop.
func TestMoonshotStream_MalformedToolCallIndex(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":"not-a-number","id":"weather_0","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":null}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck
	chunks := drainMoonshotStream(t, stream)

	if len(moonshotChunksOfType(chunks, provider.ChunkTypeError)) == 0 {
		t.Fatalf("expected at least 1 error chunk, got %#v", chunks)
	}
}

// TestMoonshotStream_ChoiceLevelUsageFallback verifies usage is read from
// choices[].usage when no top-level usage field is present.
func TestMoonshotStream_ChoiceLevelUsageFallback(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"OK"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17,"completion_tokens_details":{"reasoning_tokens":1}}}]}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck
	chunks := drainMoonshotStream(t, stream)

	finishes := moonshotChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
	usage := finishes[0].Usage
	if usage == nil || usage.InputTokens == nil || *usage.InputTokens != 12 {
		t.Fatalf("expected choice-level usage to populate finish usage, got %#v", usage)
	}
}

// TestMoonshotStream_TopLevelUsagePrecedence verifies a later top-level usage
// chunk overrides an earlier choice-level usage value.
func TestMoonshotStream_TopLevelUsagePrecedence(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"OK"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}]}

data: {"choices":[],"usage":{"prompt_tokens":99,"completion_tokens":33,"total_tokens":132}}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck
	chunks := drainMoonshotStream(t, stream)

	finishes := moonshotChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
	usage := finishes[0].Usage
	if usage == nil || usage.InputTokens == nil || *usage.InputTokens != 99 {
		t.Fatalf("expected top-level usage (99) to win over choice-level (12), got %#v", usage)
	}
}

// TestMoonshotStream_ErrorEnvelope verifies a mid-stream {"error":{...}}
// frame surfaces as a rich *providererrors.ProviderError carrying the
// Moonshot error code, and also emits a lightweight error chunk immediately.
func TestMoonshotStream_ErrorEnvelope(t *testing.T) {
	sseData := `data: {"error":{"message":"Internal server error","type":"server_error","code":"upstream_failure"}}

data: [DONE]

`
	stream := newMoonshotStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("unexpected error on first Next(): %v", err)
	}
	if chunk.Type != provider.ChunkTypeError || chunk.Text != "Internal server error" {
		t.Fatalf("expected an error chunk with the message, got %#v", chunk)
	}

	_, err = stream.Next()
	if err == nil {
		t.Fatal("expected the stream to terminate with a rich provider error")
	}
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.ErrorCode != "upstream_failure" {
		t.Errorf("expected error code 'upstream_failure', got %q", provErr.ErrorCode)
	}
	if provErr.StatusCode != 500 {
		t.Errorf("expected status code 500 for server_error, got %d", provErr.StatusCode)
	}
	if !provErr.IsRetryable() {
		t.Error("expected a server_error to be retryable")
	}
}
