package streaming

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func intPtr(v int) *int { return &v }

func chunksOfType(chunks []ToolCallChunk, chunkType provider.ChunkType) []ToolCallChunk {
	var filtered []ToolCallChunk
	for _, chunk := range chunks {
		if chunk.Type == chunkType {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func TestStreamingToolCallTrackerAccumulatesByIndex(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	chunks := tracker.Track(intPtr(0), "call_1", "get_weather", `{"ci`)
	chunks = append(chunks, tracker.Track(intPtr(0), "", "", `ty":"San Francisco"}`)...)
	chunks = append(chunks, tracker.Flush()...)

	wantTypes := []provider.ChunkType{
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputDelta,
		provider.ChunkTypeToolInputDelta,
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall,
	}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("expected %d chunks, got %d", len(wantTypes), len(chunks))
	}
	for i, wantType := range wantTypes {
		if chunks[i].Type != wantType {
			t.Fatalf("chunk[%d] type = %s, want %s", i, chunks[i].Type, wantType)
		}
	}
	chunk := chunks[len(chunks)-1]
	if chunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("expected tool-call chunk, got %s", chunk.Type)
	}
	if chunk.ToolCall.ID != "call_1" || chunk.ToolCall.ToolName != "get_weather" {
		t.Fatalf("unexpected tool call: %#v", chunk.ToolCall)
	}
	if got := chunk.ToolCall.Arguments["city"]; got != "San Francisco" {
		t.Fatalf("city = %#v, want San Francisco", got)
	}
}

func TestStreamingToolCallTrackerFallsBackToID(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(nil, "call_a", "lookup", `{"q":"`)
	tracker.Track(nil, "call_a", "", `docs"}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ID != "call_a" {
		t.Fatalf("id = %q, want call_a", toolCalls[0].ToolCall.ID)
	}
	if got := toolCalls[0].ToolCall.Arguments["q"]; got != "docs" {
		t.Fatalf("q = %#v, want docs", got)
	}
}

func TestStreamingToolCallTrackerBuffersArgumentsBeforeName(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "", `{"q":"`)
	tracker.Track(intPtr(0), "", "search", `weather"}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ToolName != "search" {
		t.Fatalf("tool name = %q, want search", toolCalls[0].ToolCall.ToolName)
	}
	if got := toolCalls[0].ToolCall.Arguments["q"]; got != "weather" {
		t.Fatalf("q = %#v, want weather", got)
	}
}

func TestStreamingToolCallTrackerFlushesIncompleteArguments(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "broken", `{"q":"unterminated`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.Arguments != nil {
		t.Fatalf("expected invalid arguments to stay nil, got %#v", toolCalls[0].ToolCall.Arguments)
	}
}

func TestStreamingToolCallTrackerPreservesProviderMetadata(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.TrackDelta(ToolCallDelta{
		Index:          intPtr(0),
		ID:             "call_1",
		Name:           "fn",
		ArgumentsDelta: `{}`,
		ProviderMetadata: map[string]interface{}{
			"google": map[string]interface{}{"thoughtSignature": "sig123"},
		},
	})

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	meta := toolCalls[0].ToolCall.ProviderMetadata
	if meta == nil {
		t.Fatal("expected provider metadata")
	}
	google, ok := meta["google"].(map[string]interface{})
	if !ok || google["thoughtSignature"] != "sig123" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
}

func TestStreamingToolCallTrackerGeneratesFallbackID(t *testing.T) {
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string { return "generated" })

	tracker.Track(intPtr(0), "", "fn", `{}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if got := toolCalls[0].ToolCall.ID; got != "generated" {
		t.Fatalf("id = %q, want generated", got)
	}
	inputEnds := chunksOfType(chunks, provider.ChunkTypeToolInputEnd)
	if len(inputEnds) != 1 || inputEnds[0].ToolCall.ID != "generated" {
		t.Fatalf("input end id = %#v, want generated", inputEnds)
	}
}

// TestStreamingToolCallTrackerReusedIndexNewID ports TS "should keep distinct
// tool calls that reuse an index" (streaming-tool-call-tracker.test.ts): a
// non-empty ID is looked up on its own, so a second delta at the same index
// but a different, non-empty ID must start a brand new tool call instead of
// merging into the first one (1bec07d).
func TestStreamingToolCallTrackerReusedIndexNewID(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "fn", `{"value":1}`)
	tracker.Track(intPtr(0), "call_2", "fn", `{"value":2}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool-call chunks, got %d: %#v", len(toolCalls), toolCalls)
	}
	if toolCalls[0].ToolCall.ID != "call_1" || toolCalls[0].ToolCall.Arguments["value"] != float64(1) {
		t.Fatalf("unexpected first tool call: %#v", toolCalls[0].ToolCall)
	}
	if toolCalls[1].ToolCall.ID != "call_2" || toolCalls[1].ToolCall.Arguments["value"] != float64(2) {
		t.Fatalf("unexpected second tool call: %#v", toolCalls[1].ToolCall)
	}
}

// TestStreamingToolCallTrackerContinuesLatestWhenIndexOmitted ports TS
// "should continue the latest tool call when an index is omitted after
// starting at %s": a delta with neither a usable ID nor an index falls back
// to whatever call was most recently tracked (1bec07d).
func TestStreamingToolCallTrackerContinuesLatestWhenIndexOmitted(t *testing.T) {
	for _, startIndex := range []*int{nil, intPtr(7)} {
		tracker := NewStreamingToolCallTracker()

		tracker.Track(startIndex, "call_1", "fn", `{"val`)
		chunks := tracker.Track(nil, "", "", `ue":1}`)
		chunks = append(chunks, tracker.Flush()...)

		toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
		if len(toolCalls) != 1 {
			t.Fatalf("startIndex=%v: expected 1 tool-call chunk, got %d", startIndex, len(toolCalls))
		}
		if toolCalls[0].ToolCall.ID != "call_1" {
			t.Fatalf("startIndex=%v: id = %q, want call_1", startIndex, toolCalls[0].ToolCall.ID)
		}
		if got := toolCalls[0].ToolCall.Arguments["value"]; got != float64(1) {
			t.Fatalf("startIndex=%v: value = %#v, want 1", startIndex, got)
		}
	}
}

// TestStreamingToolCallTrackerIndexFallbackWhenIDEmpty ports TS "should use
// the index when continuation IDs are empty": an explicit empty-string ID on
// a continuation delta must not be treated as a lookup key -- it falls back
// to the index (1bec07d).
func TestStreamingToolCallTrackerIndexFallbackWhenIDEmpty(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "fn", `{"val`)
	tracker.Track(intPtr(0), "", "", `ue":1}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", len(toolCalls))
	}
	if toolCalls[0].ToolCall.ID != "call_1" {
		t.Fatalf("id = %q, want call_1", toolCalls[0].ToolCall.ID)
	}
	if got := toolCalls[0].ToolCall.Arguments["value"]; got != float64(1) {
		t.Fatalf("value = %#v, want 1", got)
	}
}
