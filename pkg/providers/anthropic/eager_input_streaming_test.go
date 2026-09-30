package anthropic

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ---------------------------------------------------------------------------
// TestAnthropicEagerInputStreamingSerializedInTool
// Verify that a custom function tool with EagerInputStreaming=true has
// "eager_input_streaming": true in the serialized Anthropic tool map.
// ---------------------------------------------------------------------------

func TestAnthropicEagerInputStreamingSerializedInTool(t *testing.T) {
	enabled := true
	tool := types.Tool{
		Name:        "my_function",
		Description: "A test function tool",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
		},
		ProviderOptions: &ToolOptions{
			EagerInputStreaming: &enabled,
		},
	}

	converted := testPrepareToolsWire(t, []types.Tool{tool})
	if len(converted) != 1 {
		t.Fatalf("expected 1 converted tool, got %d", len(converted))
	}

	toolMap := converted[0]
	if toolMap["eager_input_streaming"] != true {
		t.Errorf("eager_input_streaming = %v, want true", toolMap["eager_input_streaming"])
	}
	// name and input_schema must still be present
	if toolMap["name"] != "my_function" {
		t.Errorf("name = %v, want my_function", toolMap["name"])
	}
}

func TestAnthropicEagerInputStreamingNotSetByDefault(t *testing.T) {
	// Without EagerInputStreaming, the field must be absent
	tool := types.Tool{
		Name:        "my_function",
		Description: "A test function tool",
		Parameters:  map[string]interface{}{"type": "object"},
	}

	converted := testPrepareToolsWire(t, []types.Tool{tool})
	toolMap := converted[0]

	if _, hasField := toolMap["eager_input_streaming"]; hasField {
		t.Error("eager_input_streaming should not be present when EagerInputStreaming is nil")
	}
}

// TestAnthropicUnrecognizedProviderToolWarnAndSkip ports the real prepareTools
// path for an "anthropic.*" + ProviderExecuted tool that carries none of the
// per-instance config prepareTools needs to recognize it (no ProviderOptions,
// so neither the builtin-type table nor the anthropicAPIMapper self-serializing
// path matches): TS's prepareTools warns "provider-defined tool <id>" and
// omits it from the wire tools array entirely (anthropic-prepare-tools.ts
// default case in the provider-tool switch; see anthropic-prepare-tools.test.ts
// "should add warnings for unsupported tools"). Replaces the old
// TestAnthropicEagerInputStreamingNotAppliedToProviderTools, which exercised
// the removed ToAnthropicFormatWithCache helper — that helper had no
// warn-and-skip branch and instead silently fell through to regular function
// tool serialization, which is not what the real request path does.
func TestAnthropicUnrecognizedProviderToolWarnAndSkip(t *testing.T) {
	tool := types.Tool{
		Name:             "anthropic.web_search_20260209",
		ProviderExecuted: true,
	}

	m := NewLanguageModel(New(Config{APIKey: "test-key"}), "claude-sonnet-4-6", nil)
	req, err := m.prepareRequest(&provider.GenerateOptions{Tools: []types.Tool{tool}}, false)
	if err != nil {
		t.Fatalf("prepareRequest() error: %v", err)
	}

	if wire, _ := req.body["tools"].([]map[string]interface{}); len(wire) != 0 {
		t.Errorf("tools = %#v, want empty (unrecognized provider tool skipped)", wire)
	}

	found := false
	for _, w := range req.warnings {
		if w.Type == "unsupported" && w.Feature == "provider-defined tool anthropic.web_search_20260209" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %#v, want an 'unsupported' warning for the unrecognized provider tool", req.warnings)
	}
}

// ---------------------------------------------------------------------------
// TestAnthropicEagerInputStreamingToolInputDeltaEvents
// Verify that the stream emits tool-input-start, tool-input-delta(×N),
// tool-input-end, and tool-call events for a regular tool_use block.
// ---------------------------------------------------------------------------

func TestAnthropicEagerInputStreamingToolInputDeltaEvents(t *testing.T) {
	body := io.NopCloser(strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_eager\",\"name\":\"search\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"golang\\\"}\" }}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":15}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	))

	stream := newAnthropicStream(body, false)
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	// Collect by type
	var starts, deltas, ends, toolCalls []*provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolInputStart:
			starts = append(starts, c)
		case provider.ChunkTypeToolInputDelta:
			deltas = append(deltas, c)
		case provider.ChunkTypeToolInputEnd:
			ends = append(ends, c)
		case provider.ChunkTypeToolCall:
			toolCalls = append(toolCalls, c)
		}
	}

	// Must have exactly 1 tool-input-start
	if len(starts) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolInputStart, got %d", len(starts))
	}
	if starts[0].ToolCall == nil {
		t.Fatal("tool-input-start ToolCall is nil")
	}
	if starts[0].ToolCall.ID != "call_eager" {
		t.Errorf("tool-input-start ID = %q, want call_eager", starts[0].ToolCall.ID)
	}
	if starts[0].ToolCall.ToolName != "search" {
		t.Errorf("tool-input-start ToolName = %q, want search", starts[0].ToolCall.ToolName)
	}

	// Must have 2 tool-input-delta chunks (one per input_json_delta event)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 ChunkTypeToolInputDelta, got %d", len(deltas))
	}
	if deltas[0].Text != `{"q":` {
		t.Errorf("delta[0].Text = %q, want {\"q\":", deltas[0].Text)
	}

	// Must have exactly 1 tool-input-end
	if len(ends) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolInputEnd, got %d", len(ends))
	}
	if ends[0].ToolCall == nil {
		t.Fatal("tool-input-end ToolCall is nil")
	}
	if ends[0].ToolCall.ID != "call_eager" {
		t.Errorf("tool-input-end ID = %q, want call_eager", ends[0].ToolCall.ID)
	}

	// Must have exactly 1 tool-call with assembled arguments
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 ChunkTypeToolCall, got %d", len(toolCalls))
	}
	tc := toolCalls[0].ToolCall
	if tc == nil {
		t.Fatal("tool-call ToolCall is nil")
	}
	if tc.ID != "call_eager" {
		t.Errorf("tool-call ID = %q, want call_eager", tc.ID)
	}
	if tc.ToolName != "search" {
		t.Errorf("tool-call ToolName = %q, want search", tc.ToolName)
	}
	if tc.Arguments["q"] != "golang" {
		t.Errorf("tool-call Arguments[q] = %v, want golang", tc.Arguments["q"])
	}

	// Order: tool-input-start must come before deltas, end before tool-call
	chunkOrder := make([]provider.ChunkType, 0, len(chunks))
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolInputStart,
			provider.ChunkTypeToolInputDelta,
			provider.ChunkTypeToolInputEnd,
			provider.ChunkTypeToolCall:
			chunkOrder = append(chunkOrder, c.Type)
		}
	}
	expected := []provider.ChunkType{
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputDelta,
		provider.ChunkTypeToolInputDelta,
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall,
	}
	if len(chunkOrder) != len(expected) {
		t.Fatalf("chunk order len = %d, want %d: %v", len(chunkOrder), len(expected), chunkOrder)
	}
	for i, ct := range expected {
		if chunkOrder[i] != ct {
			t.Errorf("chunk[%d].Type = %q, want %q", i, chunkOrder[i], ct)
		}
	}
}

func TestAnthropicToolInputStartEndWithPrePopulatedInput(t *testing.T) {
	// Deferred tool call: input pre-populated in content_block_start, no deltas.
	// Must still emit tool-input-start, tool-input-end, tool-call (no deltas).
	body := io.NopCloser(strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_pre\",\"name\":\"lookup\",\"input\":{\"key\":\"val\"}}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	))

	stream := newAnthropicStream(body, false)
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	startChunks := filterChunks(chunks, provider.ChunkTypeToolInputStart)
	deltaChunks := filterChunks(chunks, provider.ChunkTypeToolInputDelta)
	endChunks := filterChunks(chunks, provider.ChunkTypeToolInputEnd)
	callChunks := filterChunks(chunks, provider.ChunkTypeToolCall)

	if len(startChunks) != 1 {
		t.Errorf("expected 1 tool-input-start, got %d", len(startChunks))
	}
	if len(deltaChunks) != 0 {
		t.Errorf("expected 0 tool-input-delta (pre-populated input), got %d", len(deltaChunks))
	}
	if len(endChunks) != 1 {
		t.Errorf("expected 1 tool-input-end, got %d", len(endChunks))
	}
	if len(callChunks) != 1 {
		t.Errorf("expected 1 tool-call, got %d", len(callChunks))
	}
	if len(callChunks) > 0 && callChunks[0].ToolCall != nil {
		if callChunks[0].ToolCall.Arguments["key"] != "val" {
			t.Errorf("tool-call Arguments[key] = %v, want val", callChunks[0].ToolCall.Arguments["key"])
		}
	}
}

func TestAnthropicServerToolUseNoInputEvents(t *testing.T) {
	// server_tool_use (provider-executed web_search, etc.) must NOT emit
	// tool-input-start/delta/end — only tool-call at block stop.
	body := io.NopCloser(strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"srv_001\",\"name\":\"web_search\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"query\\\":\\\"test\\\"}\" }}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	))

	stream := newAnthropicStream(body, false)
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	startChunks := filterChunks(chunks, provider.ChunkTypeToolInputStart)
	deltaChunks := filterChunks(chunks, provider.ChunkTypeToolInputDelta)
	endChunks := filterChunks(chunks, provider.ChunkTypeToolInputEnd)
	callChunks := filterChunks(chunks, provider.ChunkTypeToolCall)

	if len(startChunks) != 0 {
		t.Errorf("server_tool_use must not emit tool-input-start, got %d", len(startChunks))
	}
	if len(deltaChunks) != 0 {
		t.Errorf("server_tool_use must not emit tool-input-delta, got %d", len(deltaChunks))
	}
	if len(endChunks) != 0 {
		t.Errorf("server_tool_use must not emit tool-input-end, got %d", len(endChunks))
	}
	// tool-call IS still emitted for server_tool_use
	if len(callChunks) != 1 {
		t.Errorf("expected 1 tool-call for server_tool_use, got %d", len(callChunks))
	}
}
