package anthropic

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// sseBody builds a minimal SSE body from the given lines.
// Each entry becomes:  "event: <event>\ndata: <data>\n\n"
func sseBody(events []sseEntry) io.ReadCloser {
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString("event: ")
		sb.WriteString(e.event)
		sb.WriteString("\ndata: ")
		sb.WriteString(e.data)
		sb.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(sb.String()))
}

type sseEntry struct {
	event string
	data  string
}

// drainStream reads all chunks from a stream until io.EOF, returning them in order.
func drainStream(t *testing.T, s *anthropicStream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		chunk, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// ---------------------------------------------------------------------------
// TCS-T12: Single tool call across start / delta(s) / stop → one ChunkTypeToolCall
// ---------------------------------------------------------------------------

func TestToolCallStreaming_SingleToolCall(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_abc","name":"get_weather"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"NYC\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	// Expect exactly: ChunkTypeToolCall, ChunkTypeFinish
	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall, got %d", len(toolChunks))
	}

	tc := toolChunks[0].ToolCall
	if tc == nil {
		t.Fatal("ToolCall is nil")
	}
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.ToolName != "get_weather" {
		t.Errorf("ToolCall.ToolName = %q, want %q", tc.ToolName, "get_weather")
	}
	if tc.Arguments["location"] != "NYC" {
		t.Errorf("ToolCall.Arguments[location] = %v, want NYC", tc.Arguments["location"])
	}

	// Finish chunk must also be present
	finishChunks := filterChunks(chunks, provider.ChunkTypeFinish)
	if len(finishChunks) != 1 {
		t.Errorf("expected 1 ChunkTypeFinish, got %d", len(finishChunks))
	}
	if finishChunks[0].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("FinishReason = %v, want FinishReasonToolCalls", finishChunks[0].FinishReason)
	}
}

// ---------------------------------------------------------------------------
// TCS-T13: Multi-argument tool call JSON accumulates correctly
// ---------------------------------------------------------------------------

func TestToolCallStreaming_MultiArgument(t *testing.T) {
	// Simulate Anthropic streaming {"location":"Paris","unit":"celsius"} in 3 fragments
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_multi","name":"get_weather"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"Paris\","}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"unit\":\"celsius\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall, got %d", len(toolChunks))
	}
	tc := toolChunks[0].ToolCall
	if tc.Arguments["location"] != "Paris" {
		t.Errorf("Arguments[location] = %v, want Paris", tc.Arguments["location"])
	}
	if tc.Arguments["unit"] != "celsius" {
		t.Errorf("Arguments[unit] = %v, want celsius", tc.Arguments["unit"])
	}
}

// ---------------------------------------------------------------------------
// TCS-T14: Multiple concurrent tool calls at different indexes emit separately
// ---------------------------------------------------------------------------

func TestToolCallStreaming_MultipleConcurrentToolCalls(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":20}}}`,
		},
		// Tool 1 at index 0
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"tool_alpha"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"x\":1}"}}`,
		},
		// Tool 2 at index 1
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_2","name":"tool_beta"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"y\":2}"}}`,
		},
		// Stop both
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":1}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 2 {
		t.Fatalf("expected 2 ChunkTypeToolCall chunks, got %d", len(toolChunks))
	}

	// Build a map by tool name for order-independent assertions
	byName := map[string]*types.ToolCall{}
	for _, c := range toolChunks {
		byName[c.ToolCall.ToolName] = c.ToolCall
	}

	alpha := byName["tool_alpha"]
	if alpha == nil {
		t.Fatal("no chunk for tool_alpha")
	}
	if alpha.ID != "call_1" {
		t.Errorf("tool_alpha.ID = %q, want call_1", alpha.ID)
	}
	if alpha.Arguments["x"] != float64(1) {
		t.Errorf("tool_alpha.Arguments[x] = %v, want 1", alpha.Arguments["x"])
	}

	beta := byName["tool_beta"]
	if beta == nil {
		t.Fatal("no chunk for tool_beta")
	}
	if beta.ID != "call_2" {
		t.Errorf("tool_beta.ID = %q, want call_2", beta.ID)
	}
	if beta.Arguments["y"] != float64(2) {
		t.Errorf("tool_beta.Arguments[y] = %v, want 2", beta.Arguments["y"])
	}
}

// ---------------------------------------------------------------------------
// TCS-T15: Text and tool call blocks interleaved work correctly
// ---------------------------------------------------------------------------

func TestToolCallStreaming_TextAndToolInterleaved(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		},
		// Text block at index 0
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"text","id":"","name":""}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check that for you."}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		// Tool block at index 1
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_xyz","name":"lookup"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"weather\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":1}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	textChunks := filterChunks(chunks, provider.ChunkTypeText)
	if len(textChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeText, got %d", len(textChunks))
	}
	if textChunks[0].Text != "Let me check that for you." {
		t.Errorf("text chunk = %q, want 'Let me check that for you.'", textChunks[0].Text)
	}

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall, got %d", len(toolChunks))
	}
	if toolChunks[0].ToolCall.ToolName != "lookup" {
		t.Errorf("tool name = %q, want lookup", toolChunks[0].ToolCall.ToolName)
	}
	if toolChunks[0].ToolCall.Arguments["q"] != "weather" {
		t.Errorf("tool arg q = %v, want weather", toolChunks[0].ToolCall.Arguments["q"])
	}

	// Text must appear before tool call in chunk order
	textIdx := chunkIndex(chunks, provider.ChunkTypeText)
	toolIdx := chunkIndex(chunks, provider.ChunkTypeToolCall)
	if textIdx >= toolIdx {
		t.Errorf("text chunk (%d) should appear before tool chunk (%d)", textIdx, toolIdx)
	}
}

// ---------------------------------------------------------------------------
// Gap 1 — redacted_thinking blocks are treated as reasoning (no panic/error)
// ---------------------------------------------------------------------------

func TestToolCallStreaming_RedactedThinkingNoop(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"<redacted>"}}`,
		},
		// No deltas — redacted blocks have no streaming content
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream) // must not panic or error

	// No tool or reasoning chunks expected — just a finish chunk
	if len(filterChunks(chunks, provider.ChunkTypeToolCall)) != 0 {
		t.Error("unexpected ChunkTypeToolCall for redacted_thinking block")
	}
	if len(filterChunks(chunks, provider.ChunkTypeFinish)) != 1 {
		t.Errorf("expected 1 ChunkTypeFinish, got %d", len(filterChunks(chunks, provider.ChunkTypeFinish)))
	}
}

// ---------------------------------------------------------------------------
// Gap 2 — server_tool_use blocks emit ChunkTypeToolCall
// ---------------------------------------------------------------------------

func TestToolCallStreaming_ServerToolUse_WebSearch(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtu_search","name":"web_search"}}`,
		},
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"golang\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall for web_search, got %d", len(toolChunks))
	}
	tc := toolChunks[0].ToolCall
	if tc.ID != "srvtu_search" {
		t.Errorf("ToolCall.ID = %q, want srvtu_search", tc.ID)
	}
	if tc.ToolName != "web_search" {
		t.Errorf("ToolCall.ToolName = %q, want web_search", tc.ToolName)
	}
	if tc.Arguments["query"] != "golang" {
		t.Errorf("ToolCall.Arguments[query] = %v, want golang", tc.Arguments["query"])
	}
}

func TestToolCallStreaming_ServerToolUse_BashCodeExecution(t *testing.T) {
	// bash_code_execution: tool name normalised to "code_execution",
	// and the first delta gets {"type":"bash_code_execution", prepended.
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtu_bash","name":"bash_code_execution"}}`,
		},
		// First delta: starts with '{' — should get type prefix prepended
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"code\":\"print(1)\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall for bash_code_execution, got %d", len(toolChunks))
	}
	tc := toolChunks[0].ToolCall
	// Emitted tool name is normalized
	if tc.ToolName != "code_execution" {
		t.Errorf("ToolCall.ToolName = %q, want code_execution", tc.ToolName)
	}
	// The type discriminator must be present in the parsed arguments
	if tc.Arguments["type"] != "bash_code_execution" {
		t.Errorf("ToolCall.Arguments[type] = %v, want bash_code_execution", tc.Arguments["type"])
	}
	if tc.Arguments["code"] != "print(1)" {
		t.Errorf("ToolCall.Arguments[code] = %v, want print(1)", tc.Arguments["code"])
	}
}

// ---------------------------------------------------------------------------
// Gap 3 — initial input in tool_use content_block_start (deferred tool calls)
// ---------------------------------------------------------------------------

func TestToolCallStreaming_InitialInputInContentBlockStart(t *testing.T) {
	// When the full tool input arrives in content_block_start (no deltas),
	// content_block_stop must still emit a correct ChunkTypeToolCall.
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_pre","name":"run_task","input":{"task":"deploy","env":"prod"}}}`,
		},
		// No input_json_delta events — input was fully provided in start
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall, got %d", len(toolChunks))
	}
	tc := toolChunks[0].ToolCall
	if tc.ID != "call_pre" {
		t.Errorf("ToolCall.ID = %q, want call_pre", tc.ID)
	}
	if tc.ToolName != "run_task" {
		t.Errorf("ToolCall.ToolName = %q, want run_task", tc.ToolName)
	}
	if tc.Arguments["task"] != "deploy" {
		t.Errorf("ToolCall.Arguments[task] = %v, want deploy", tc.Arguments["task"])
	}
	if tc.Arguments["env"] != "prod" {
		t.Errorf("ToolCall.Arguments[env] = %v, want prod", tc.Arguments["env"])
	}
}

// ---------------------------------------------------------------------------
// Gap 4 — empty input_json_delta does not corrupt the accumulated buffer
// ---------------------------------------------------------------------------

func TestToolCallStreaming_EmptyPartialJSONSkipped(t *testing.T) {
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
		},
		{
			event: "content_block_start",
			data:  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_empty","name":"noop"}}`,
		},
		// Empty delta — must be skipped
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`,
		},
		// Real delta follows
		{
			event: "content_block_delta",
			data:  `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"k\":\"v\"}"}}`,
		},
		{
			event: "content_block_stop",
			data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall, got %d", len(toolChunks))
	}
	if toolChunks[0].ToolCall.Arguments["k"] != "v" {
		t.Errorf("ToolCall.Arguments[k] = %v, want v", toolChunks[0].ToolCall.Arguments["k"])
	}
}

// ---------------------------------------------------------------------------
// Gap 6 — message_start pre-populated tool_use blocks emit ChunkTypeToolCall
// ---------------------------------------------------------------------------

func TestToolCallStreaming_MessageStartPrePopulatedToolCall(t *testing.T) {
	// Deferred programmatic tool calls arrive fully assembled in
	// message_start.message.content rather than via content_block_delta events.
	body := sseBody([]sseEntry{
		{
			event: "message_start",
			data:  `{"type":"message_start","message":{"usage":{"input_tokens":20},"content":[{"type":"tool_use","id":"call_deferred","name":"exec","input":{"cmd":"ls"}}]}}`,
		},
		// No content_block events at all — tool was pre-populated
		{
			event: "message_delta",
			data:  `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		},
		{
			event: "message_stop",
			data:  `{"type":"message_stop"}`,
		},
	})

	stream := newAnthropicStream(body, false)
	chunks := drainStream(t, stream)

	toolChunks := filterChunks(chunks, provider.ChunkTypeToolCall)
	if len(toolChunks) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall from message_start content, got %d", len(toolChunks))
	}
	tc := toolChunks[0].ToolCall
	if tc.ID != "call_deferred" {
		t.Errorf("ToolCall.ID = %q, want call_deferred", tc.ID)
	}
	if tc.ToolName != "exec" {
		t.Errorf("ToolCall.ToolName = %q, want exec", tc.ToolName)
	}
	if tc.Arguments["cmd"] != "ls" {
		t.Errorf("ToolCall.Arguments[cmd] = %v, want ls", tc.Arguments["cmd"])
	}

	// The finish chunk must follow
	if len(filterChunks(chunks, provider.ChunkTypeFinish)) != 1 {
		t.Errorf("expected 1 ChunkTypeFinish after pre-populated tool call")
	}
	// Tool chunk must precede finish chunk
	if chunkIndex(chunks, provider.ChunkTypeToolCall) > chunkIndex(chunks, provider.ChunkTypeFinish) {
		t.Error("tool chunk should appear before finish chunk")
	}
}

// ---------------------------------------------------------------------------
// TCS-T16: Integration test stub (skips without ANTHROPIC_API_KEY)
// ---------------------------------------------------------------------------

func TestToolCallStreaming_Integration(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping integration test: ANTHROPIC_API_KEY not set")
	}

	cfg := Config{
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		BaseURL: DefaultBaseURL,
	}
	prov := New(cfg)
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	// Verify the stream returns at least one ChunkTypeToolCall when a tool is called
	_ = model
	t.Log("Integration test stub — live call not yet implemented in unit test context")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func filterChunks(chunks []*provider.StreamChunk, chunkType provider.ChunkType) []*provider.StreamChunk {
	var out []*provider.StreamChunk
	for _, c := range chunks {
		if c.Type == chunkType {
			out = append(out, c)
		}
	}
	return out
}

func chunkIndex(chunks []*provider.StreamChunk, chunkType provider.ChunkType) int {
	for i, c := range chunks {
		if c.Type == chunkType {
			return i
		}
	}
	return -1
}
