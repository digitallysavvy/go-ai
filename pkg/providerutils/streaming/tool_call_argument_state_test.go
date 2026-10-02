package streaming

import "testing"

// Ports TS startsWithStructuredValue tests
// (provider-utils/src/streaming-tool-call-argument-state.test.ts).
func TestStartsWithStructuredValue(t *testing.T) {
	trueCases := []string{"{}", "  {", "[]", "\n["}
	for _, v := range trueCases {
		if !StartsWithStructuredValue(v) {
			t.Errorf("StartsWithStructuredValue(%q) = false, want true", v)
		}
	}

	falseCases := []string{"", "   ", "1", `"value"`}
	for _, v := range falseCases {
		if StartsWithStructuredValue(v) {
			t.Errorf("StartsWithStructuredValue(%q) = true, want false", v)
		}
	}
}

// Ports TS StreamingToolCallArgumentState tests
// (provider-utils/src/streaming-tool-call-argument-state.test.ts).
func TestStreamingToolCallArgumentState_TracksStructuredValueAcrossDeltas(t *testing.T) {
	state := NewStreamingToolCallArgumentState(`  {"value":`)
	if state.HasCompleteStructuredValue() {
		t.Fatal("expected incomplete before closing brace")
	}
	state.Append("1}")
	if !state.HasCompleteStructuredValue() {
		t.Fatal("expected complete after closing brace")
	}
}

func TestStreamingToolCallArgumentState_TracksNestedObjectsAndArrays(t *testing.T) {
	state := NewStreamingToolCallArgumentState(`[{"value":{"items":[1,2]}}]`)
	if !state.HasCompleteStructuredValue() {
		t.Fatal("expected complete nested structure")
	}
}

func TestStreamingToolCallArgumentState_IgnoresStructuralCharsInsideStrings(t *testing.T) {
	state := NewStreamingToolCallArgumentState(`{"value":"braces: } ] { ["}`)
	if !state.HasCompleteStructuredValue() {
		t.Fatal("expected complete; bracket-like chars inside a string must not affect structure")
	}
}

func TestStreamingToolCallArgumentState_HandlesEscapedQuotesAcrossDeltas(t *testing.T) {
	state := NewStreamingToolCallArgumentState(`{"value":"escaped quote: \"`)
	if state.HasCompleteStructuredValue() {
		t.Fatal("expected incomplete: the escaped quote must not close the string")
	}
	state.Append(` still in string"}`)
	if !state.HasCompleteStructuredValue() {
		t.Fatal("expected complete after the real closing quote and brace")
	}
}

func TestStreamingToolCallArgumentState_CanBeginAfterWhitespaceOnlyDelta(t *testing.T) {
	state := NewStreamingToolCallArgumentState("  ")
	state.Append("[")
	if state.HasCompleteStructuredValue() {
		t.Fatal("expected incomplete with an open array")
	}
	state.Append("]")
	if !state.HasCompleteStructuredValue() {
		t.Fatal("expected complete once the array closes")
	}
}

func TestStreamingToolCallArgumentState_ScalarIsNeverComplete(t *testing.T) {
	state := NewStreamingToolCallArgumentState("12")
	if state.HasCompleteStructuredValue() {
		t.Fatal("a scalar argument must never be treated as a complete structured value")
	}
}

func TestStreamingToolCallArgumentState_MismatchedStructureNeverRecoversAsComplete(t *testing.T) {
	state := NewStreamingToolCallArgumentState(`{"value":]`)
	state.Append("}")
	if state.HasCompleteStructuredValue() {
		t.Fatal("a mismatched closing bracket must never be treated as complete")
	}
}
