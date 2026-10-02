package streaming

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

func TestOpenAICompatStream_IncludeRawChunksEmitsRawBeforeParsedChunk(t *testing.T) {
	sseData := `data: {"id":"raw-1","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"raw-2","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	stream.IncludeRawChunks = true
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	wantTypes := []provider.ChunkType{
		provider.ChunkTypeRaw,
		provider.ChunkTypeText,
		provider.ChunkTypeRaw,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("expected %d chunks, got %d", len(wantTypes), len(chunks))
	}
	for i, wantType := range wantTypes {
		if chunks[i].Type != wantType {
			t.Fatalf("chunk[%d] type = %s, want %s", i, chunks[i].Type, wantType)
		}
	}
	raw, ok := chunks[0].Raw.(map[string]interface{})
	if !ok {
		t.Fatalf("raw chunk type = %T, want map[string]interface{}", chunks[0].Raw)
	}
	if raw["id"] != "raw-1" {
		encoded, _ := json.Marshal(raw)
		t.Fatalf("raw chunk = %s, want id raw-1", encoded)
	}
	if chunks[1].Text != "Hello" {
		t.Fatalf("text chunk = %q, want Hello", chunks[1].Text)
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

func TestOpenAICompatStream_ToolCallAndFinishSameChunk(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"calc","arguments":"{\"op\":\"add\"}"}}]},"finish_reason":"tool_calls"}]}

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
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %d", len(finishes))
	}
	if finishes[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want %q", finishes[0].FinishReason, types.FinishReasonToolCalls)
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

func TestOpenAICompatStream_EmitsToolDeltasAsSoonAsNameArrives(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_late","type":"function","function":{"arguments":"{\""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"q"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"docs\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	first, err := stream.Next()
	if err != nil {
		t.Fatalf("first Next error: %v", err)
	}
	if first.Type != provider.ChunkTypeToolInputStart || first.ToolCall.ID != "call_late" || first.ToolCall.ToolName != "lookup" {
		t.Fatalf("first chunk = %#v, want live tool-input-start once name arrives", first)
	}
	second, err := stream.Next()
	if err != nil {
		t.Fatalf("second Next error: %v", err)
	}
	if second.Type != provider.ChunkTypeToolInputDelta || second.Text != `{"q` {
		t.Fatalf("second chunk = %#v, want buffered plus named argument delta", second)
	}
	third, err := stream.Next()
	if err != nil {
		t.Fatalf("third Next error: %v", err)
	}
	if third.Type != provider.ChunkTypeToolInputDelta || third.Text != `":"docs"}` {
		t.Fatalf("third chunk = %#v, want subsequent live argument delta", third)
	}
}

func TestOpenAICompatStream_ErrorsWhenToolCallNameNeverArrives(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_missing","type":"function","function":{"arguments":"{}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %d in %#v", len(errorChunks), chunks)
	}
	if errorChunks[0].Text != "Expected 'function.name' to be a string." {
		t.Fatalf("error text = %q", errorChunks[0].Text)
	}
	if toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall); len(toolCalls) != 0 {
		t.Fatalf("expected no tool-call chunks, got %#v", toolCalls)
	}
	if finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 0 {
		t.Fatalf("expected no finish chunks after invalid tool call, got %#v", finishes)
	}
}

// TestOpenAICompatStream_BlankToolCallNameDoesNotAbortSiblingCalls proves the
// Flush fix alongside TestOpenAICompatStream_ErrorsWhenToolCallNameNeverArrives:
// a gateway that sends one usable call plus one with an explicitly blank
// name (TS's "should ignore a blank function name without preventing prior
// calls from finalizing") must finish the turn successfully with just the
// valid call, not error out and drop everything.
func TestOpenAICompatStream_BlankToolCallNameDoesNotAbortSiblingCalls(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"valid_tool","arguments":"{}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"","arguments":"{}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	if errs := compatChunksOfType(chunks, provider.ChunkTypeError); len(errs) != 0 {
		t.Fatalf("expected no error chunks, got %#v", errs)
	}
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.ID != "call_1" || toolCalls[0].ToolCall.ToolName != "valid_tool" {
		t.Fatalf("expected only call_1/valid_tool, got %#v", toolCalls)
	}
	if finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", finishes)
	}
}

// TestOpenAICompatStream_KeepsIDlessToolCallsDistinctOnReusedIndex ports the
// OpenAI-compatible regression added by TS #18445 ("should keep id-less
// tool calls distinct when the index is reused"): three id-less tool_calls
// deltas sharing index 0 (read_file / write_file / read_file) must remain
// three distinct tool calls, not merge into one or two.
func TestOpenAICompatStream_KeepsIDlessToolCallsDistinctOnReusedIndex(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"read_file","arguments":"{\"path\":\"p0\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"write_file","arguments":"{\"path\":\"p1\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"read_file","arguments":"{\"path\":\"p2\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 3 {
		t.Fatalf("expected 3 tool-call chunks, got %d in %#v", len(toolCalls), chunks)
	}

	wantNames := []string{"read_file", "write_file", "read_file"}
	wantInputs := []string{`{"path":"p0"}`, `{"path":"p1"}`, `{"path":"p2"}`}
	ids := map[string]bool{}
	for i, tc := range toolCalls {
		if tc.ToolCall.ToolName != wantNames[i] {
			t.Fatalf("toolCalls[%d].ToolName = %q, want %q", i, tc.ToolCall.ToolName, wantNames[i])
		}
		if tc.ToolCall.RawArguments != wantInputs[i] {
			t.Fatalf("toolCalls[%d].RawArguments = %q, want %q", i, tc.ToolCall.RawArguments, wantInputs[i])
		}
		if strings.TrimSpace(tc.ToolCall.ID) == "" {
			t.Fatalf("toolCalls[%d].ID is blank", i)
		}
		ids[tc.ToolCall.ID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 distinct tool call ids, got %#v", ids)
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

// TestOpenAICompatStream_DefersFinishForTrailingUsageChunk verifies that a
// trailing choices-less usage event (stream_options.include_usage) arriving
// after finish_reason is merged into the finish chunk, and that finish is
// still the last chunk emitted — matching TS openai-compatible, which only
// enqueues `finish` in flush(), after the whole stream (including this tail
// chunk) has been consumed.
func TestOpenAICompatStream_DefersFinishForTrailingUsageChunk(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	wantTypes := []provider.ChunkType{
		provider.ChunkTypeText,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("chunk sequence = %#v, want length %d", chunks, len(wantTypes))
	}
	for i, want := range wantTypes {
		if chunks[i].Type != want {
			t.Fatalf("chunk[%d] = %v, want %v (full: %#v)", i, chunks[i].Type, want, chunks)
		}
	}
	finish := chunks[len(chunks)-1]
	if finish.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish reason = %v, want stop", finish.FinishReason)
	}
	if finish.Usage == nil || finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 4 {
		t.Fatalf("finish usage = %#v, want inputTokens 4", finish.Usage)
	}
	if finish.Usage.OutputTokens == nil || *finish.Usage.OutputTokens != 3 {
		t.Fatalf("finish usage = %#v, want outputTokens 3", finish.Usage)
	}
	if finish.Usage.TotalTokens == nil || *finish.Usage.TotalTokens != 7 {
		t.Fatalf("finish usage = %#v, want totalTokens 7", finish.Usage)
	}
}

// TestOpenAICompatStream_UsageOnSameEventAsFinishReason verifies the common
// case where usage arrives on the same SSE event as finish_reason.
func TestOpenAICompatStream_UsageOnSameEventAsFinishReason(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].Usage == nil || finishes[0].Usage.TotalTokens == nil || *finishes[0].Usage.TotalTokens != 3 {
		t.Fatalf("finish usage = %#v, want totalTokens 3", finishes[0].Usage)
	}
}

func TestOpenAICompatStream_ParseErrorEmitsErrorChunkAfterRaw(t *testing.T) {
	sseData := `data: {"choices":[

data: [DONE]

`
	stream := newTestStream(sseData)
	stream.IncludeRawChunks = true
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("raw chunk error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("error chunk error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("second chunk type = %v, want error", chunk.Type)
	}
	if !strings.Contains(chunk.Text, "failed to parse stream chunk") {
		t.Fatalf("error text = %q, want parse failure", chunk.Text)
	}
}

func TestOpenAICompatStream_ProviderErrorEventEmitsErrorChunkAfterRaw(t *testing.T) {
	sseData := `data: {"error":{"message":"provider failed"}}

data: [DONE]

`
	stream := newTestStream(sseData)
	stream.IncludeRawChunks = true
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("raw chunk error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeRaw {
		t.Fatalf("first chunk type = %v, want raw", chunk.Type)
	}

	chunk, err = stream.Next()
	if err != nil {
		t.Fatalf("error chunk error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("second chunk type = %v, want error", chunk.Type)
	}
	if chunk.Text != "provider failed" {
		t.Fatalf("error text = %q, want provider failed", chunk.Text)
	}
}

// TestOpenAICompatStream_TruncatedStreamEmitsErrorAndErrorFinish covers C1-4:
// a stream that ends (EOF or [DONE]) after text deltas but without ever
// observing a finish_reason must be treated as an error, matching TS's
// flush() `if (finishReason == null)` branch (openai-compatible-chat-
// language-model.ts:725-734, commit d68139c3bb): an InvalidResponseDataError
// "error" chunk, followed by a "finish" chunk with the unified error finish
// reason.
func TestOpenAICompatStream_TruncatedStreamEmitsErrorAndErrorFinish(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	wantTypes := []provider.ChunkType{
		provider.ChunkTypeText,
		provider.ChunkTypeText,
		provider.ChunkTypeError,
		provider.ChunkTypeFinish,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("chunk sequence = %#v, want length %d", chunks, len(wantTypes))
	}
	for i, want := range wantTypes {
		if chunks[i].Type != want {
			t.Fatalf("chunk[%d] = %v, want %v (full: %#v)", i, chunks[i].Type, want, chunks)
		}
	}
	errChunk := chunks[2]
	if errChunk.Text != "Response stream ended without a finish reason." {
		t.Fatalf("error text = %q", errChunk.Text)
	}
	if errChunk.Err == nil || !providererrors.IsInvalidResponseDataError(errChunk.Err) {
		t.Fatalf("error chunk Err = %#v, want *InvalidResponseDataError", errChunk.Err)
	}
	finish := chunks[3]
	if finish.FinishReason != types.FinishReasonError {
		t.Fatalf("finish reason = %v, want error", finish.FinishReason)
	}
}

// TestOpenAICompatStream_TruncatedStreamViaDoneSentinel covers the same gap
// via an explicit [DONE] sentinel instead of a bare reader EOF -- both are
// "clean" stream ends in TS's terms (the ReadableStream completing).
func TestOpenAICompatStream_TruncatedStreamViaDoneSentinel(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %#v", chunks)
	}
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].FinishReason != types.FinishReasonError {
		t.Fatalf("finish reason = %v, want error", finishes[0].FinishReason)
	}
}

// TestOpenAICompatStream_ToolCallNameErrorSuppressesTruncatedStreamError
// verifies that a mid-stream tool-call-name error is not doubled up with the
// separate "truncated stream" error when the underlying reader then hits EOF
// with no finish chunk ever pending -- TS's toolCallTracker.flush() throw
// aborts before flush()'s finishReason==null check is ever reached.
func TestOpenAICompatStream_ToolCallNameErrorSuppressesTruncatedStreamError(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_missing","type":"function","function":{"arguments":"{}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %#v", chunks)
	}
	if finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 0 {
		t.Fatalf("expected no finish chunks, got %#v", finishes)
	}
}

// TestOpenAICompatStream_ParseErrorStillEmitsFinishChunk verifies that a
// "soft" error (a JSON parse failure) does not suppress the terminal finish
// chunk once the stream cleanly ends afterward. TS's transform() sets
// finishReason to the unified "error" reason and returns normally on a parse
// failure (openai-compatible-chat-language-model.ts:574-575); flush() then
// still runs to completion, emitting the terminal `finish` chunk with that
// already-set error reason -- it just skips the separate "no finish_reason
// observed" error since finishReason is already non-null.
func TestOpenAICompatStream_ParseErrorStillEmitsFinishChunk(t *testing.T) {
	sseData := `data: {"choices":[

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %#v", chunks)
	}
	if !strings.Contains(errorChunks[0].Text, "failed to parse stream chunk") {
		t.Fatalf("error text = %q, want parse failure", errorChunks[0].Text)
	}
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].FinishReason != types.FinishReasonError {
		t.Fatalf("finish reason = %v, want error", finishes[0].FinishReason)
	}
	// The separate "missing finish reason" error must not be doubled up.
	if errorChunks[0].Text == "Response stream ended without a finish reason." {
		t.Fatalf("truncated-stream error should not fire after a parse failure: %#v", chunks)
	}
}

// TestOpenAICompatStream_ProviderErrorEventStillEmitsFinishChunk is the same
// check as TestOpenAICompatStream_ParseErrorStillEmitsFinishChunk for a
// mid-stream provider "error" field instead of a parse failure (openai-
// compatible-chat-language-model.ts:582-586).
func TestOpenAICompatStream_ProviderErrorEventStillEmitsFinishChunk(t *testing.T) {
	sseData := `data: {"error":{"message":"provider failed"}}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %#v", chunks)
	}
	if errorChunks[0].Text != "provider failed" {
		t.Fatalf("error text = %q, want provider failed", errorChunks[0].Text)
	}
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].FinishReason != types.FinishReasonError {
		t.Fatalf("finish reason = %v, want error", finishes[0].FinishReason)
	}
}

// TestOpenAICompatStream_TruncatedStreamFlushesPendingToolCall verifies that
// a truncated stream (no finish_reason ever observed) still flushes any
// tool-call fragments buffered in the tracker before the missing-finish-
// reason error/finish pair, matching TS flush()'s unconditional
// `toolCallTracker.flush()` call ahead of its `finishReason == null` check
// (openai-compatible-chat-language-model.ts:711-725).
func TestOpenAICompatStream_TruncatedStreamFlushesPendingToolCall(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":null}]}

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	toolCalls := compatChunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %#v", chunks)
	}
	if toolCalls[0].ToolCall.ID != "call_1" {
		t.Fatalf("tool call id = %q, want call_1", toolCalls[0].ToolCall.ID)
	}
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 || errorChunks[0].Text != "Response stream ended without a finish reason." {
		t.Fatalf("error chunks = %#v", errorChunks)
	}
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 || finishes[0].FinishReason != types.FinishReasonError {
		t.Fatalf("finish chunks = %#v", finishes)
	}
	// The tool call must be flushed (and any open tool-input block closed)
	// ahead of the error/finish chunks, matching TS's flush() order.
	if chunks[len(chunks)-1].Type != provider.ChunkTypeFinish {
		t.Fatalf("finish chunk should be last: %#v", chunks)
	}
	toolCallIdx, errIdx := -1, -1
	for i, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			toolCallIdx = i
		}
		if c.Type == provider.ChunkTypeError {
			errIdx = i
		}
	}
	if toolCallIdx == -1 || errIdx == -1 || toolCallIdx > errIdx {
		t.Fatalf("expected tool-call chunk before error chunk, got %#v", chunks)
	}
}

// TestOpenAICompatStream_TruncatedStreamWithMissingToolCallNameSuppressesFinish
// verifies that a truncated stream whose only pending tool call never
// received a function.name still surfaces exactly the tool-call-tracker
// error (not the separate "missing finish reason" error) and emits no finish
// chunk at all, matching TS's toolCallTracker.flush() throw aborting flush()
// before its terminal `finish` enqueue.
func TestOpenAICompatStream_TruncatedStreamWithMissingToolCallNameSuppressesFinish(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_missing","type":"function","function":{"arguments":"{}"}}]},"finish_reason":null}]}

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	errorChunks := compatChunksOfType(chunks, provider.ChunkTypeError)
	if len(errorChunks) != 1 {
		t.Fatalf("expected 1 error chunk, got %#v", chunks)
	}
	if errorChunks[0].Text != "Expected 'function.name' to be a string." {
		t.Fatalf("error text = %q", errorChunks[0].Text)
	}
	if finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish); len(finishes) != 0 {
		t.Fatalf("expected no finish chunks, got %#v", finishes)
	}
}

// TestOpenAICompatStream_RawFinishReason verifies that the terminal finish
// chunk carries the raw provider finish_reason string alongside the unified
// FinishReason, mirroring TS openai-compatible-chat-language-model.ts's
// `finishReason: { unified: mapOpenAICompatibleFinishReason(choice.finish_reason),
// raw: choice.finish_reason ?? undefined }` (the same-event finish path).
func TestOpenAICompatStream_RawFinishReason(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("finish reason = %v, want stop", finishes[0].FinishReason)
	}
	if finishes[0].RawFinishReason != "stop" {
		t.Fatalf("raw finish reason = %q, want %q", finishes[0].RawFinishReason, "stop")
	}
}

// TestOpenAICompatStream_RawFinishReason_DeferredTrailingUsage verifies the
// raw finish reason is preserved when the finish chunk is deferred to
// endStream (a trailing choices-less usage-only event after finish_reason).
func TestOpenAICompatStream_RawFinishReason_DeferredTrailingUsage(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"length"}]}

data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}

data: [DONE]

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].FinishReason != types.FinishReasonLength {
		t.Fatalf("finish reason = %v, want length", finishes[0].FinishReason)
	}
	if finishes[0].RawFinishReason != "length" {
		t.Fatalf("raw finish reason = %q, want %q", finishes[0].RawFinishReason, "length")
	}
}

// TestOpenAICompatStream_RawFinishReason_EmptyWhenNeverObserved verifies
// that when no finish_reason is ever observed (truncated stream), the
// synthesized error-finish chunk carries no raw finish reason, matching TS's
// `raw: undefined` default.
func TestOpenAICompatStream_RawFinishReason_EmptyWhenNeverObserved(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

`
	stream := newTestStream(sseData)
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	finishes := compatChunksOfType(chunks, provider.ChunkTypeFinish)
	if len(finishes) != 1 {
		t.Fatalf("expected 1 finish chunk, got %#v", chunks)
	}
	if finishes[0].RawFinishReason != "" {
		t.Fatalf("raw finish reason = %q, want empty", finishes[0].RawFinishReason)
	}
}

// TestOpenAICompatStream_NoStackGrowthOnLongChunklessRun is a regression test
// for BF5 (bug-review R6-1/R6-4): OpenAICompatStream.Next() used to recurse
// via `return s.Next()` on every chunkless/unrecognised SSE event (empty
// events, in-progress tool-call deltas, a finish event with queued chunks,
// etc). Go does not eliminate that tail call, so a long run of such events
// within one external Next() call grew the goroutine stack without bound,
// eventually crashing the process with an unrecoverable `fatal error: stack
// overflow` -- not a panic, not recover()-able.
//
// This feeds 250,000 empty/unrecognised SSE events (each hitting the
// "Empty or unrecognised event" skip branch), then one real text chunk,
// then [DONE], in a subprocess with debug.SetMaxStack lowered so a
// regression would crash deterministically well within the test's 2s
// budget instead of needing gigabytes of real stack.
func TestOpenAICompatStream_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runOpenAICompatStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestOpenAICompatStream_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless SSE events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runOpenAICompatStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 250_000
	var sb strings.Builder
	sb.Grow(n*12 + 128)
	for i := 0; i < n; i++ {
		// An empty event (no choices, no usage, no error) hits the "Empty or
		// unrecognised event — skip and fetch the next one" branch.
		sb.WriteString("data: {}\n\n")
	}
	sb.WriteString(`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n")
	sb.WriteString("data: [DONE]\n\n")

	s := newTestStream(sb.String())
	chunk, err := s.Next()
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error:", err)
		return
	}
	if chunk == nil || chunk.Type != provider.ChunkTypeText || chunk.Text != "hello" {
		fmt.Printf("BF5_CHILD_FAIL: unexpected chunk: %+v\n", chunk)
		return
	}
	fmt.Println("BF5_CHILD_OK")
}
