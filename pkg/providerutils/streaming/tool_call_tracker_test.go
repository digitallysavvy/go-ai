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

// --- TS #18445 regressions (streaming-tool-call-tracker.test.ts) ---
//
// These port the new test cases added by TS commit e3605f637a ("fix:
// prevent unreliable streamed tool-call labels from aborting, corrupting,
// losing, or misordering calls", #18445). TS's processDelta throws
// synchronously for a function.name that is genuinely absent (as opposed to
// present-but-blank, which it silently ignores); Track/TrackDelta cannot
// return an error, so Go always defers: the delta is buffered without
// emitting chunks, and either promoted once a correlated delta supplies a
// name, or reported as a ChunkTypeError from Flush if one never arrives (see
// tool_call_tracker.go's TrackDelta/processExistingToolCall/finishToolCall
// doc comments). Both of the TS "ignore"/"throw" tests below are therefore
// ported to assert on the surfaced error, not a panic.

func toolCallNames(chunks []ToolCallChunk) []string {
	var names []string
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolCall) {
		names = append(names, c.ToolCall.ToolName)
	}
	return names
}

func toolCallInputs(chunks []ToolCallChunk) []string {
	var inputs []string
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolCall) {
		inputs = append(inputs, c.ToolCall.RawArguments)
	}
	return inputs
}

func toolCallIDs(chunks []ToolCallChunk) []string {
	var ids []string
	for _, c := range chunksOfType(chunks, provider.ChunkTypeToolCall) {
		ids = append(ids, c.ToolCall.ID)
	}
	return ids
}

// ports "should throw when function.name is missing": Go never panics;
// the delta is buffered and surfaces as an error chunk at flush instead.
func TestStreamingToolCallTrackerIgnoresNewCallMissingName(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	chunks := tracker.TrackDelta(ToolCallDelta{Index: intPtr(0), ID: "call_1"})
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for a nameless new call, got %#v", chunks)
	}

	flushed := tracker.Flush()
	errs := chunksOfType(flushed, provider.ChunkTypeError)
	if len(errs) != 1 || errs[0].Text != "Expected 'function.name' to be a string." {
		t.Fatalf("expected one function.name error chunk, got %#v", flushed)
	}
}

// ports "should ignore a blank function name without preventing prior calls
// from finalizing".
func TestStreamingToolCallTrackerIgnoresBlankNameNewCall(t *testing.T) {
	for _, name := range []string{"", "   "} {
		tracker := NewStreamingToolCallTracker()

		tracker.Track(intPtr(0), "call_1", "valid_tool", `{"value":1}`)
		tracker.Track(intPtr(1), "call_2", name, `{"value":2}`)

		chunks := tracker.Flush()
		toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
		if len(toolCalls) != 1 {
			t.Fatalf("name=%q: expected 1 tool-call chunk, got %#v", name, chunks)
		}
		if toolCalls[0].ToolCall.ID != "call_1" || toolCalls[0].ToolCall.ToolName != "valid_tool" {
			t.Fatalf("name=%q: unexpected tool call: %#v", name, toolCalls[0].ToolCall)
		}
		// The whole point of "ignore": the blank-name call must not fail the
		// turn that valid_tool otherwise completed successfully (TS never
		// enqueues anything for it, let alone an error).
		if errs := chunksOfType(chunks, provider.ChunkTypeError); len(errs) != 0 {
			t.Fatalf("name=%q: expected no error chunks, got %#v", name, errs)
		}
	}
}

// A blank-name call with no successful sibling in the same flush still has
// nothing usable to report -- but it must not silently vanish either, since
// that would hide a genuinely malformed response. This stays on the fatal
// path (see Flush), matching TestStreamingToolCallTrackerIgnoresNewCallMissingName
// and the OpenAICompatStream truncated-stream regressions.
func TestStreamingToolCallTrackerBlankNameAloneStillErrors(t *testing.T) {
	tracker := NewStreamingToolCallTracker()
	tracker.Track(intPtr(0), "call_1", "", `{"value":1}`)

	chunks := tracker.Flush()
	errs := chunksOfType(chunks, provider.ChunkTypeError)
	if len(errs) != 1 || errs[0].Text != "Expected 'function.name' to be a string." {
		t.Fatalf("expected one function.name error chunk, got %#v", chunks)
	}
}

// ports "should retain continuation arguments for a $description".
func TestStreamingToolCallTrackerRetainsContinuationForBlankNameLabel(t *testing.T) {
	cases := []struct {
		name         string
		continueID   string
		continueIdx  *int
		continueName string
	}{
		{name: "blank name with matching id", continueID: "call_1", continueName: ""},
		{name: "whitespace-only name with matching index", continueIdx: intPtr(0), continueName: "   "},
	}
	for _, tc := range cases {
		tracker := NewStreamingToolCallTracker()
		tracker.Track(intPtr(0), "call_1", "read_file", `{"pa`)
		tracker.TrackDelta(ToolCallDelta{Index: tc.continueIdx, ID: tc.continueID, Name: tc.continueName, ArgumentsDelta: `th":"a"}`})

		chunks := tracker.Flush()
		toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
		if len(toolCalls) != 1 {
			t.Fatalf("%s: expected 1 tool-call chunk, got %#v", tc.name, chunks)
		}
		if toolCalls[0].ToolCall.RawArguments != `{"path":"a"}` {
			t.Fatalf("%s: input = %q, want {\"path\":\"a\"}", tc.name, toolCalls[0].ToolCall.RawArguments)
		}
	}
}

// ports "should keep id-less calls distinct when an index is reused and
// type is omitted" -- the exact OpenAI-compatible regression this commit
// fixes.
func TestStreamingToolCallTrackerKeepsIDlessCallsDistinctOnReusedIndex(t *testing.T) {
	var n int
	generated := []string{"generated-1", "generated-2", "generated-3"}
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string {
		id := generated[n]
		n++
		return id
	})

	tracker.Track(intPtr(0), "", "read_file", `{"path":"p0"}`)
	tracker.Track(intPtr(0), "", "write_file", `{"path":"p1"}`)
	tracker.Track(intPtr(0), "", "read_file", `{"path":"p2"}`)

	chunks := tracker.Flush()
	if got, want := toolCallNames(chunks), []string{"read_file", "write_file", "read_file"}; !equalStrings(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	if got, want := toolCallInputs(chunks), []string{`{"path":"p0"}`, `{"path":"p1"}`, `{"path":"p2"}`}; !equalStrings(got, want) {
		t.Fatalf("inputs = %v, want %v", got, want)
	}
	if got, want := toolCallIDs(chunks), []string{"generated-1", "generated-2", "generated-3"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

// ports "should keep complete same-name calls distinct with $description and
// a reused index".
func TestStreamingToolCallTrackerKeepsCompleteSameNameCallsDistinct(t *testing.T) {
	for _, id := range []string{"", "dup"} {
		tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string { return "generated-id" })

		tracker.Track(intPtr(0), id, "same_tool", `{"value":1}`)
		tracker.Track(intPtr(0), id, "same_tool", `{"value":2}`)

		chunks := tracker.Flush()
		inputs := toolCallInputs(chunks)
		if want := []string{`{"value":1}`, `{"value":2}`}; !equalStrings(inputs, want) {
			t.Fatalf("id=%q: inputs = %v, want %v", id, inputs, want)
		}
		ids := toolCallIDs(chunks)
		if ids[0] == ids[1] {
			t.Fatalf("id=%q: expected distinct ids, got %v", id, ids)
		}
	}
}

// ports "should keep a partial same-name call distinct with $description and
// a reused index".
func TestStreamingToolCallTrackerKeepsPartialSameNameCallDistinct(t *testing.T) {
	for _, id := range []string{"", "dup"} {
		tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string { return "generated-id" })

		tracker.Track(intPtr(0), id, "same_tool", `{"value":1}`)
		tracker.Track(intPtr(0), id, "same_tool", `{"value":`)

		chunks := tracker.Flush()
		inputs := toolCallInputs(chunks)
		if want := []string{`{"value":1}`, `{"value":`}; !equalStrings(inputs, want) {
			t.Fatalf("id=%q: inputs = %v, want %v", id, inputs, want)
		}
		ids := toolCallIDs(chunks)
		if ids[0] == ids[1] {
			t.Fatalf("id=%q: expected distinct ids, got %v", id, ids)
		}
	}
}

// ports "should keep interleaved same-name calls with distinct ids and a
// reused index separate".
func TestStreamingToolCallTrackerKeepsInterleavedSameNameCallsSeparate(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "same_tool", `{"value":`)
	tracker.Track(intPtr(0), "call_2", "same_tool", `{"value":2}`)
	tracker.Track(intPtr(0), "call_1", "", `1}`)

	chunks := tracker.Flush()
	if got, want := toolCallIDs(chunks), []string{"call_1", "call_2"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if got, want := toolCallInputs(chunks), []string{`{"value":1}`, `{"value":2}`}; !equalStrings(got, want) {
		t.Fatalf("inputs = %v, want %v", got, want)
	}
}

// ports "should ignore an index-only continuation after the index is
// reused".
func TestStreamingToolCallTrackerIgnoresIndexOnlyContinuationAfterReuse(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "first", `{"value":1}`)
	tracker.Track(intPtr(0), "call_2", "second", `{"value":2}`)
	tracker.Track(intPtr(0), "", "", `{"unattributed":true}`)

	chunks := tracker.Flush()
	if got, want := toolCallIDs(chunks), []string{"call_1", "call_2"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if got, want := toolCallInputs(chunks), []string{`{"value":1}`, `{"value":2}`}; !equalStrings(got, want) {
		t.Fatalf("inputs = %v, want %v", got, want)
	}
}

// ports "should use the index when continuation ids are blank".
func TestStreamingToolCallTrackerUsesIndexWhenContinuationIDBlank(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "read_file", `{"pa`)
	tracker.Track(intPtr(0), "   ", "", `th":"a"}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.RawArguments != `{"path":"a"}` {
		t.Fatalf("unexpected result: %#v", chunks)
	}
}

// ports "should generate unique ids for blank and repeated ids".
func TestStreamingToolCallTrackerGeneratesUniqueIDsForBlankAndRepeated(t *testing.T) {
	generated := []string{"generated-1", "generated-2"}
	n := 0
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string {
		id := generated[n]
		n++
		return id
	})

	tracker.Track(intPtr(0), "", "read_file", `{}`)
	tracker.Track(intPtr(1), "dup", "read_file", `{}`)
	tracker.Track(intPtr(2), "dup", "write_file", `{}`)

	chunks := tracker.Flush()
	if got, want := toolCallIDs(chunks), []string{"generated-1", "dup", "generated-2"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

// ports "should keep same-name calls with repeated ids and distinct indices
// separate".
func TestStreamingToolCallTrackerKeepsRepeatedIDDistinctIndicesSeparate(t *testing.T) {
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string { return "generated-id" })

	tracker.Track(intPtr(0), "dup", "same_tool", `{"value":0}`)
	tracker.Track(intPtr(1), "dup", "same_tool", `{"value":1}`)

	chunks := tracker.Flush()
	if got, want := toolCallIDs(chunks), []string{"dup", "generated-id"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

// ports "should preserve nonblank ids and function names exactly".
func TestStreamingToolCallTrackerPreservesNonblankIDsAndNamesExactly(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), " spaced ", " same_tool ", `{"value":`)
	tracker.Track(intPtr(0), " spaced ", "", `0}`)
	tracker.Track(intPtr(1), "spaced", " same_tool ", `{"value":1}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool-call chunks, got %#v", chunks)
	}
	if toolCalls[0].ToolCall.ID != " spaced " || toolCalls[0].ToolCall.ToolName != " same_tool " {
		t.Fatalf("unexpected first tool call: %#v", toolCalls[0].ToolCall)
	}
	if toolCalls[1].ToolCall.ID != "spaced" || toolCalls[1].ToolCall.ToolName != " same_tool " {
		t.Fatalf("unexpected second tool call: %#v", toolCalls[1].ToolCall)
	}
}

// ports "should create bounded unique ids when generateId returns
// duplicates".
func TestStreamingToolCallTrackerCreatesBoundedUniqueIDsOnDuplicates(t *testing.T) {
	calls := 0
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string {
		calls++
		return "generated-id"
	})

	tracker.Track(intPtr(0), "", "first", `{}`)
	tracker.Track(intPtr(1), "", "second", `{}`)
	tracker.Track(intPtr(2), "", "third", `{}`)

	chunks := tracker.Flush()
	if calls != 3 {
		t.Fatalf("generateId called %d times, want 3", calls)
	}
	if got, want := toolCallIDs(chunks), []string{"generated-id", "generated-id-1", "generated-id-2"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

// ports "should create usable ids when generateId returns blank values".
func TestStreamingToolCallTrackerCreatesUsableIDsForBlankGenerated(t *testing.T) {
	calls := 0
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string {
		calls++
		return "   "
	})

	tracker.Track(intPtr(0), "", "first", `{}`)
	tracker.Track(intPtr(1), "", "second", `{}`)

	chunks := tracker.Flush()
	if calls != 2 {
		t.Fatalf("generateId called %d times, want 2", calls)
	}
	if got, want := toolCallIDs(chunks), []string{"tool-call", "tool-call-1"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

// ports "should ignore unattributable deltas when multiple calls are
// active".
func TestStreamingToolCallTrackerIgnoresUnattributableDeltaWithMultipleActive(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "read_file", `{"path":"a"}`)
	tracker.Track(intPtr(1), "call_2", "write_file", `{"path":"b"}`)
	tracker.Track(nil, "", "", `{"unattributed":true}`)

	chunks := tracker.Flush()
	if got, want := toolCallIDs(chunks), []string{"call_1", "call_2"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if got, want := toolCallInputs(chunks), []string{`{"path":"a"}`, `{"path":"b"}`}; !equalStrings(got, want) {
		t.Fatalf("inputs = %v, want %v", got, want)
	}
}

// ports "should ignore an ambiguous continuation for a repeated id".
func TestStreamingToolCallTrackerIgnoresAmbiguousContinuationForRepeatedID(t *testing.T) {
	tracker := NewStreamingToolCallTrackerWithIDGenerator(func() string { return "generated-id" })

	tracker.Track(intPtr(0), "dup", "read_file", `{"path":"a"}`)
	tracker.Track(intPtr(1), "dup", "write_file", `{"path":"b"}`)
	tracker.Track(nil, "dup", "", `{"unattributed":true}`)

	chunks := tracker.Flush()
	if got, want := toolCallIDs(chunks), []string{"dup", "generated-id"}; !equalStrings(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if got, want := toolCallInputs(chunks), []string{`{"path":"a"}`, `{"path":"b"}`}; !equalStrings(got, want) {
		t.Fatalf("inputs = %v, want %v", got, want)
	}
}

// ports "should use a matching name and index for an id-less continuation".
func TestStreamingToolCallTrackerUsesMatchingNameAndIndexForIDlessContinuation(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "read_file", `{"pa`)
	tracker.Track(intPtr(0), "", "read_file", `th":"a"}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.ID != "call_1" || toolCalls[0].ToolCall.RawArguments != `{"path":"a"}` {
		t.Fatalf("unexpected result: %#v", chunks)
	}
}

// ports "should use the index when a continuation has an unexpected id".
func TestStreamingToolCallTrackerUsesIndexForUnexpectedContinuationID(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "read_file", `{"pa`)
	tracker.Track(intPtr(0), "unexpected", "", `th":"a"}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.ID != "call_1" || toolCalls[0].ToolCall.RawArguments != `{"path":"a"}` {
		t.Fatalf("unexpected result: %#v", chunks)
	}
}

// ports "should continue a call when its id changes but its index and name
// match".
func TestStreamingToolCallTrackerContinuesWhenIDChangesButIndexAndNameMatch(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "read_file", `{"pa`)
	tracker.Track(intPtr(0), "unexpected", "read_file", `th":"a"}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.ID != "call_1" || toolCalls[0].ToolCall.RawArguments != `{"path":"a"}` {
		t.Fatalf("unexpected result: %#v", chunks)
	}
}

// ports "should continue a call when all labels repeat after a parsable
// argument prefix".
func TestStreamingToolCallTrackerContinuesWhenLabelsRepeatAfterParsablePrefix(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "calculate", "1")
	tracker.Track(intPtr(0), "call_1", "calculate", "2")

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.RawArguments != "12" {
		t.Fatalf("unexpected result: %#v", chunks)
	}
}

// ports "should continue a structured argument when repeated labels precede
// a nested object".
func TestStreamingToolCallTrackerContinuesStructuredArgWithRepeatedLabels(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(0), "call_1", "calculate", `{"value":`)
	tracker.Track(intPtr(0), "call_1", "calculate", `{"nested":true}}`)

	chunks := tracker.Flush()
	toolCalls := chunksOfType(chunks, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.RawArguments != `{"value":{"nested":true}}` {
		t.Fatalf("unexpected result: %#v", chunks)
	}
}

// ports "should emit tool calls in index order".
func TestStreamingToolCallTrackerEmitsToolCallsInIndexOrder(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(intPtr(1), "call_1", "second", `{}`)
	tracker.Track(intPtr(0), "call_0", "first", `{}`)

	chunks := tracker.Flush()
	if got, want := toolCallNames(chunks), []string{"first", "second"}; !equalStrings(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

// ports "should preserve insertion order when calls mix present and omitted
// indices".
func TestStreamingToolCallTrackerPreservesInsertionOrderForMixedIndices(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	tracker.Track(nil, "call_without_index", "first", `{}`)
	tracker.Track(intPtr(0), "call_with_index", "second", `{}`)

	chunks := tracker.Flush()
	if got, want := toolCallNames(chunks), []string{"first", "second"}; !equalStrings(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

// ports "should throw when function.name is missing from a new call" (see
// the Go-divergence note above TestStreamingToolCallTrackerIgnoresNewCallMissingName).
func TestStreamingToolCallTrackerThrowsWhenNameMissingFromNewCall(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	chunks := tracker.TrackDelta(ToolCallDelta{Index: intPtr(0), ID: "call_1"})
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks, got %#v", chunks)
	}
	flushed := tracker.Flush()
	errs := chunksOfType(flushed, provider.ChunkTypeError)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error chunk, got %#v", flushed)
	}
}

// ports TS's new OpenAI-compatible regression at the tracker level: a name
// that only arrives on a later, index-correlated delta must still produce a
// usable tool call (TestOpenAICompatStream_EmitsToolDeltasAsSoonAsNameArrives
// in openai_compat_stream_test.go covers the same scenario end-to-end
// through the SSE stream).
func TestStreamingToolCallTrackerPromotesPendingCallOnceNameArrives(t *testing.T) {
	tracker := NewStreamingToolCallTracker()

	chunks := tracker.Track(intPtr(0), "call_late", "", `{"`)
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks before the name arrives, got %#v", chunks)
	}

	chunks = tracker.Track(intPtr(0), "", "lookup", "q")
	wantTypes := []provider.ChunkType{provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta}
	if len(chunks) != len(wantTypes) {
		t.Fatalf("promotion chunks = %#v, want types %v", chunks, wantTypes)
	}
	for i, want := range wantTypes {
		if chunks[i].Type != want {
			t.Fatalf("promotion chunk[%d] = %v, want %v", i, chunks[i].Type, want)
		}
	}
	if chunks[0].ToolCall.ID != "call_late" || chunks[0].ToolCall.ToolName != "lookup" {
		t.Fatalf("unexpected tool-input-start: %#v", chunks[0].ToolCall)
	}
	if chunks[1].Text != `{"q` {
		t.Fatalf("combined initial delta = %q, want {\"q", chunks[1].Text)
	}

	chunks = tracker.Track(intPtr(0), "", "", `":"docs"}`)
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkTypeToolInputDelta || chunks[0].Text != `":"docs"}` {
		t.Fatalf("live delta after promotion = %#v", chunks)
	}

	flushed := tracker.Flush()
	toolCalls := chunksOfType(flushed, provider.ChunkTypeToolCall)
	if len(toolCalls) != 1 || toolCalls[0].ToolCall.RawArguments != `{"q":"docs"}` {
		t.Fatalf("unexpected final result: %#v", flushed)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
