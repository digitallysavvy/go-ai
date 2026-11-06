package streaming

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS processExistingToolCall emits a tool-input-delta whenever
// function.arguments != null, including an empty string.
func TestStreamingToolCallTrackerEmitsEmptyDeltaWhenArgumentsPresent(t *testing.T) {
	tracker := NewStreamingToolCallTracker()
	tracker.TrackDelta(ToolCallDelta{Index: intPtr(0), ID: "c1", Name: "f", ArgumentsDelta: `{"a"`, ArgumentsPresent: true})
	present := tracker.TrackDelta(ToolCallDelta{Index: intPtr(0), ArgumentsPresent: true})
	if got := chunksOfType(present, provider.ChunkTypeToolInputDelta); len(got) != 1 || got[0].Text != "" {
		t.Fatalf("present empty arguments -> %#v, want one empty delta", present)
	}
	absent := tracker.TrackDelta(ToolCallDelta{Index: intPtr(0)})
	if len(absent) != 0 {
		t.Fatalf("absent arguments -> %#v, want nothing", absent)
	}
}
