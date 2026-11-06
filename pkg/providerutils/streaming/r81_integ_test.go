package streaming

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS "should throw when function.name is missing from a new call": the
// tracker fails at once; it accepts no later delta and flushes nothing.
func TestR81integTrackerAbsentNameFailsAtOnce(t *testing.T) {
	zero, two := 0, 2
	tr := NewStreamingToolCallTracker()
	// A named call before the failure is never finalized either.
	tr.TrackDelta(ToolCallDelta{Index: &two, ID: "c2", Name: "t", ArgumentsDelta: "{}", ArgumentsPresent: true})
	chunks := tr.TrackDelta(ToolCallDelta{Index: &zero, ID: "c1", ArgumentsDelta: "{}", ArgumentsPresent: true, NameAbsent: true})
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkTypeError || chunks[0].Text != "Expected 'function.name' to be a string." || !tr.Failed() {
		t.Fatalf("chunks = %+v failed = %v, want the function.name error", chunks, tr.Failed())
	}
	if got := tr.TrackDelta(ToolCallDelta{Index: &zero, ID: "c1", Name: "t", ArgumentsDelta: "{}", ArgumentsPresent: true}); got != nil {
		t.Fatalf("after failure TrackDelta = %+v, want nil", got)
	}
	if got := tr.Flush(); got != nil {
		t.Fatalf("after failure Flush = %+v, want nil", got)
	}

	// The type check fails the tracker the same way (TS throws).
	tr = NewStreamingToolCallTracker()
	tr.SetTypeValidation(TypeValidationRequired)
	if chunks := tr.TrackDelta(ToolCallDelta{Index: &zero, Name: "t", Type: "custom"}); len(chunks) != 1 || chunks[0].Type != provider.ChunkTypeError || !tr.Failed() {
		t.Fatalf("type check: chunks = %+v failed = %v", chunks, tr.Failed())
	}
	if got := tr.TrackDelta(ToolCallDelta{Index: &zero, Name: "t", Type: "function", ArgumentsDelta: "{}"}); got != nil {
		t.Fatalf("after type failure TrackDelta = %+v, want nil", got)
	}

	// Buffered (openai-compatible pendingToolCalls): an indexed nameless
	// call waits for its name, and one never named is an error at Flush
	// even when another call succeeds; TS flush() throws before
	// toolCallTracker.flush(), so the named call is not finalized either.
	tr = NewStreamingToolCallTracker()
	tr.BufferNamelessIndexedCalls()
	one := 1
	if got := tr.TrackDelta(ToolCallDelta{Index: &zero, ID: "c0", ArgumentsDelta: "{}", ArgumentsPresent: true, NameAbsent: true}); got != nil || tr.Failed() {
		t.Fatalf("buffered: chunks = %+v failed = %v", got, tr.Failed())
	}
	tr.TrackDelta(ToolCallDelta{Index: &one, ID: "c1", Name: "t", ArgumentsDelta: "{}", ArgumentsPresent: true})
	var errs, calls int
	for _, c := range tr.Flush() {
		switch c.Type {
		case provider.ChunkTypeError:
			errs++
		case provider.ChunkTypeToolCall:
			calls++
		}
	}
	if errs != 1 || calls != 0 {
		t.Fatalf("flush: %d errors %d calls, want 1 and 0", errs, calls)
	}
}
