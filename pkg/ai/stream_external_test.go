package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestNewStreamTextResultFromParts_TwoSteps ports the harness.md §3 "P0
// prerequisite" exit criterion: build a 2-step result from canned chunks
// with no provider.LanguageModel call, and verify accessors match what
// StreamText itself would produce for the equivalent multi-step sequence.
func TestNewStreamTextResultFromParts_TwoSteps(t *testing.T) {
	t.Parallel()

	one := int64(1)
	two := int64(2)
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		// Step 1: a tool call, no visible text.
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
			ID: "call_1", ToolName: "search", Arguments: map[string]interface{}{"q": "go"},
		}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
			ToolCallID: "call_1", ToolName: "search", Result: "results",
		}},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: &one, OutputTokens: &one, TotalTokens: &two}},
		// Step 2: the final text answer.
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "The "},
		{Type: provider.ChunkTypeText, Text: "answer."},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: &one, OutputTokens: &one, TotalTokens: &two}},
	})

	var finishedResult *StreamTextResult
	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{
		Provider: "harness", ModelID: "claude-code",
		OnEnd: func(r *StreamTextResult) { finishedResult = r },
	})

	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if finishedResult != result {
		t.Fatal("OnEnd was not called with the result")
	}
	if result.Status() != StreamStatusDone {
		t.Fatalf("Status() = %v, want done", result.Status())
	}

	if got, want := result.Text(), "The answer."; got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
	if got := result.FinishReason(); got != types.FinishReasonStop {
		t.Fatalf("FinishReason() = %q, want stop", got)
	}

	steps := result.Steps()
	if len(steps) != 2 {
		t.Fatalf("len(Steps()) = %d, want 2", len(steps))
	}
	if steps[0].StepNumber != 0 || steps[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("steps[0] = %+v", steps[0])
	}
	if len(steps[0].ToolCalls) != 1 || steps[0].ToolCalls[0].ToolName != "search" {
		t.Fatalf("steps[0].ToolCalls = %+v", steps[0].ToolCalls)
	}
	if len(steps[0].ToolResults) != 1 {
		t.Fatalf("steps[0].ToolResults = %+v", steps[0].ToolResults)
	}
	if steps[1].StepNumber != 1 || steps[1].Text != "The answer." || steps[1].FinishReason != types.FinishReasonStop {
		t.Fatalf("steps[1] = %+v", steps[1])
	}

	toolCalls := result.ToolCalls()
	if len(toolCalls) != 1 || toolCalls[0].ToolName != "search" {
		t.Fatalf("ToolCalls() = %+v", toolCalls)
	}

	usage := result.Usage()
	if usage.TotalTokens == nil || *usage.TotalTokens != 4 {
		t.Fatalf("Usage().TotalTokens = %v, want 4 (summed across both steps)", usage.TotalTokens)
	}
}

// TestNewStreamTextResultFromParts_ToUIMessageStream verifies the P0
// requirement that "the UI-message-stream conversion must work on it": a
// multi-step external result must produce start-step/finish-step pairs for
// each step, not a premature terminal finish after the first step.
func TestNewStreamTextResultFromParts_ToUIMessageStream(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "Step one."},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonToolCalls},
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "Step two."},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{
		Provider: "harness", ModelID: "claude-code",
	})

	uiChunks, errs := result.ToUIMessageStream(context.Background())

	var types_ []string
	var texts []string
	for c := range uiChunks {
		ty, _ := c["type"].(string)
		types_ = append(types_, ty)
		if ty == "text-delta" {
			if d, ok := c["delta"].(string); ok {
				texts = append(texts, d)
			}
		}
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("ToUIMessageStream error = %v", err)
	}

	startSteps, finishSteps := 0, 0
	for _, ty := range types_ {
		switch ty {
		case "start-step":
			startSteps++
		case "finish-step":
			finishSteps++
		}
	}
	if startSteps != 2 {
		t.Fatalf("start-step count = %d, want 2: %v", startSteps, types_)
	}
	if finishSteps != 2 {
		t.Fatalf("finish-step count = %d, want 2 (one per step, not just the terminal one): %v", finishSteps, types_)
	}
	// The overall "finish" event must appear exactly once, after both steps.
	finishCount := 0
	for _, ty := range types_ {
		if ty == "finish" {
			finishCount++
		}
	}
	if finishCount != 1 {
		t.Fatalf("finish count = %d, want 1: %v", finishCount, types_)
	}
	if len(texts) != 2 || texts[0] != "Step one." || texts[1] != "Step two." {
		t.Fatalf("text deltas = %v, want both steps' text preserved in order", texts)
	}
}

// TestNewStreamTextResultFromParts_PropagatesSourceError verifies that a
// non-EOF error from src surfaces through Err()/ReadAll().
func TestNewStreamTextResultFromParts_PropagatesSourceError(t *testing.T) {
	t.Parallel()

	boom := context.DeadlineExceeded
	src := testutil.NewMockTextStreamWithError(boom)

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	if err := result.Err(); err != boom {
		t.Fatalf("Err() = %v, want %v", err, boom)
	}
}

// TestNewStreamTextResultFromParts_ErrorChunkPreservesPartialStep verifies
// that a ChunkTypeError arriving mid-step (with no FinishStep/Finish before
// it) still flushes whatever the step accumulated so far onto
// Steps()/Text()/ToolCalls() instead of silently discarding it, and stops
// consuming src immediately rather than relying on src returning io.EOF.
func TestNewStreamTextResultFromParts_ErrorChunkPreservesPartialStep(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "partial"},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
			ID: "call_1", ToolName: "search", Arguments: map[string]interface{}{},
		}},
		{Type: provider.ChunkTypeError, Text: "bridge crashed"},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	select {
	case <-result.processingDone:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeExternalParts did not stop after an error chunk")
	}

	if err := result.Err(); err == nil || err.Error() != "bridge crashed" {
		t.Fatalf("Err() = %v, want %q", err, "bridge crashed")
	}
	if result.Text() != "partial" {
		t.Errorf("Text() = %q, want %q", result.Text(), "partial")
	}
	if len(result.ToolCalls()) != 1 || result.ToolCalls()[0].ToolName != "search" {
		t.Errorf("ToolCalls() = %+v, want one call to 'search'", result.ToolCalls())
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("Steps() = %d, want 1 (the partial step should still be flushed)", len(steps))
	}
	if steps[0].FinishReason != types.FinishReasonError {
		t.Errorf("Steps()[0].FinishReason = %q, want %q", steps[0].FinishReason, types.FinishReasonError)
	}
}

// TestNewStreamTextResultFromParts_TerminalFinishOverridesTotalUsage ports
// the harness.md WG4 / TS 57e0a59 behavior: a terminal ChunkTypeFinish that
// carries no new step content of its own (every step was already closed by
// its own ChunkTypeFinishStep) must not sum its Usage onto the locally
// accumulated total, and must not append a spurious empty extra step —
// its Usage instead OVERRIDES the total, matching
// HarnessStreamTextResult.finish()'s `this.accumulatedUsage =
// asLanguageModelUsage(input.totalUsage)`.
func TestNewStreamTextResultFromParts_TerminalFinishOverridesTotalUsage(t *testing.T) {
	t.Parallel()

	one := int64(1)
	hundred := int64(100)
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		// Step 0.
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: &one, OutputTokens: &one, TotalTokens: &one}},
		// Step 1.
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: " there"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: &one, OutputTokens: &one, TotalTokens: &one}},
		// Terminal finish: no new content, carries the bridge's own
		// (larger, e.g. cache-inclusive) totalUsage that must win outright.
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: &hundred, OutputTokens: &hundred, TotalTokens: &hundred}},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if got, want := result.Text(), "hi there"; got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
	steps := result.Steps()
	if len(steps) != 2 {
		t.Fatalf("len(Steps()) = %d, want 2 (no phantom step for the terminal boundary): %+v", len(steps), steps)
	}
	usage := result.Usage()
	if usage.TotalTokens == nil || *usage.TotalTokens != 100 {
		t.Fatalf("Usage().TotalTokens = %v, want 100 (overridden by the terminal finish, not summed to 102)", usage.TotalTokens)
	}
	if got := result.FinishReason(); got != types.FinishReasonStop {
		t.Fatalf("FinishReason() = %q, want stop", got)
	}
}

// TestNewStreamTextResultFromParts_SingleEmptyStepStillAppended guards
// against the terminal-ChunkTypeFinish-as-boundary optimization above
// misfiring for a non-harness caller whose only step happens to produce no
// visible content and is closed directly by ChunkTypeFinish with no
// preceding ChunkTypeFinishStep (stepNumber never advanced past 0). That
// step must still be appended to Steps() and its usage recorded, exactly as
// it would have been before the harness-specific override was added.
func TestNewStreamTextResultFromParts_SingleEmptyStepStillAppended(t *testing.T) {
	t.Parallel()

	one := int64(1)
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: &one, OutputTokens: &one, TotalTokens: &one}},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("len(Steps()) = %d, want 1 (a genuinely empty lone step must still be recorded)", len(steps))
	}
	usage := result.Usage()
	if usage.TotalTokens == nil || *usage.TotalTokens != 1 {
		t.Fatalf("Usage().TotalTokens = %v, want 1", usage.TotalTokens)
	}
}

// TestNewStreamTextResultFromParts_TerminalFinishAppliesFinishReasonAndMetadata
// verifies that when the terminal-boundary override path fires (harness
// shape), FinishReason, RawFinishReason and ProviderMetadata carried on that
// chunk are still applied to the result rather than being dropped along with
// its (absent) content.
func TestNewStreamTextResultFromParts_TerminalFinishAppliesFinishReasonAndMetadata(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength, RawFinishReason: "max_tokens", ProviderMetadata: json.RawMessage(`{"harness":{"turnID":"t1"}}`)},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if got := result.FinishReason(); got != types.FinishReasonLength {
		t.Fatalf("FinishReason() = %q, want %q", got, types.FinishReasonLength)
	}
	if got := result.RawFinishReason(); got != "max_tokens" {
		t.Fatalf("RawFinishReason() = %q, want %q", got, "max_tokens")
	}
	md := result.ProviderMetadata()
	if md == nil {
		t.Fatal("ProviderMetadata() = nil, want the terminal finish chunk's metadata")
	}
	var decoded map[string]map[string]interface{}
	if err := json.Unmarshal(md, &decoded); err != nil {
		t.Fatalf("ProviderMetadata() did not decode: %v", err)
	}
	if decoded["harness"]["turnID"] != "t1" {
		t.Fatalf("ProviderMetadata() = %s, want harness.turnID = t1", md)
	}
}

// TestNewStreamTextResultFromParts_AbortChunkPreservesPartialStep ports TS
// 86a84c9's "settle a turn aborted by the caller's abortSignal with an
// `abort` stream part instead of an [error]" contract at the
// NewStreamTextResultFromParts level: a ChunkTypeAbort arriving mid-step
// (like ChunkTypeError) still flushes accumulated text/tool calls onto
// Steps()/Text() instead of discarding them, uses FinishReasonOther (not
// FinishReasonError, since this is a clean stop, not a failure), and Err()
// is still non-nil so an awaiting caller does not hang.
func TestNewStreamTextResultFromParts_AbortChunkPreservesPartialStep(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "partial"},
		{Type: provider.ChunkTypeAbort, AbortReason: "stopped by caller"},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	err := result.Err()
	if err == nil || !strings.Contains(err.Error(), "stopped by caller") {
		t.Fatalf("Err() = %v, want it to mention %q", err, "stopped by caller")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Err() = %v, want errors.Is(err, context.Canceled) so downstream abort-detection classifies it correctly regardless of which ctx later reads the result", err)
	}
	if result.Text() != "partial" {
		t.Errorf("Text() = %q, want %q (partial content preserved)", result.Text(), "partial")
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("Steps() = %d, want 1 (the partial step should still be flushed)", len(steps))
	}
	if steps[0].FinishReason != types.FinishReasonOther {
		t.Errorf("Steps()[0].FinishReason = %q, want %q (a clean stop, not an error)", steps[0].FinishReason, types.FinishReasonOther)
	}
}

// TestNewStreamTextResultFromParts_AbortChunkSurfacesAsUIMessageAbort verifies
// that ToUIMessageStream forwards a ChunkTypeAbort as an "abort" UI chunk
// rather than an "error" one, matching TS's "toUIMessageStream emits an
// abort chunk [and] skips onError" expectation (TS 86a84c9).
func TestNewStreamTextResultFromParts_AbortChunkSurfacesAsUIMessageAbort(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeAbort, AbortReason: "stopped by caller"},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})

	uiChunks, errs := result.ToUIMessageStream(context.Background())
	var types_ []string
	for c := range uiChunks {
		ty, _ := c["type"].(string)
		types_ = append(types_, ty)
	}
	if err, ok := <-errs; ok && err != nil {
		t.Fatalf("ToUIMessageStream error = %v", err)
	}

	sawAbort, sawError := false, false
	for _, ty := range types_ {
		if ty == "abort" {
			sawAbort = true
		}
		if ty == "error" {
			sawError = true
		}
	}
	if !sawAbort {
		t.Fatalf("chunk types = %v, want an \"abort\" chunk", types_)
	}
	if sawError {
		t.Fatalf("chunk types = %v, want no \"error\" chunk", types_)
	}
}

// TestNewStreamTextResultFromParts_RespectsContextCancellation verifies that
// an already-cancelled ctx stops consumption instead of hanging forever.
func TestNewStreamTextResultFromParts_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "unreachable"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})

	result := NewStreamTextResultFromParts(ctx, src, ExternalStreamOptions{})

	select {
	case <-result.processingDone:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeExternalParts did not stop after ctx cancellation")
	}
	if result.Err() == nil {
		t.Fatal("Err() = nil, want context.Canceled")
	}
}

// TestNewStreamTextResultFromParts_Output ports the harness.md WG4
// structured-output wiring (TS 62a9c2a): ExternalStreamOptions.Output, when
// it satisfies outputProcessor, parses the final step's accumulated text
// into Output()/OutputErr() once the stream settles, and PartialOutput()
// updates (deduplicated) as text arrives within that step — mirroring the
// identical block in the regular StreamText path this reuses (see
// stream.go's opts.Output handling).
func TestNewStreamTextResultFromParts_Output(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: `{"greeting":`},
		{Type: provider.ChunkTypeText, Text: `"hi"}`},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{
		Provider: "harness", ModelID: "claude-code",
		Output: JSONOutput(JSONOutputOptions{Name: "greeting"}),
	})

	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if err := result.OutputErr(); err != nil {
		t.Fatalf("OutputErr() = %v, want nil", err)
	}
	parsed, ok := result.Output().(map[string]interface{})
	if !ok {
		t.Fatalf("Output() = %#v (%T), want map[string]interface{}", result.Output(), result.Output())
	}
	if parsed["greeting"] != "hi" {
		t.Fatalf("Output()[\"greeting\"] = %v, want %q", parsed["greeting"], "hi")
	}
	// The dedup rule (only publish when the JSON representation changes)
	// still lets a valid, growing partial publish more than once; assert
	// only that some non-nil partial was captured along the way.
	if result.PartialOutput() == nil {
		t.Fatal("PartialOutput() = nil, want a parsed partial from streaming text")
	}
}

// TestNewStreamTextResultFromParts_OutputIgnoredWhenUnset verifies that
// ExternalStreamOptions.Output left at its zero value (nil) leaves
// Output()/OutputErr()/PartialOutput() at their own zero values, and that a
// value not satisfying outputProcessor is silently ignored exactly like
// StreamTextOptions.Output (both mirror the TS `settings.output == null`
// short-circuit).
func TestNewStreamTextResultFromParts_OutputIgnoredWhenUnset(t *testing.T) {
	t.Parallel()

	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	})

	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{
		Provider: "harness", ModelID: "claude-code", Output: "not-an-output-spec",
	})

	if err := result.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if result.Output() != nil {
		t.Fatalf("Output() = %#v, want nil", result.Output())
	}
	if result.OutputErr() != nil {
		t.Fatalf("OutputErr() = %v, want nil", result.OutputErr())
	}
	if result.PartialOutput() != nil {
		t.Fatalf("PartialOutput() = %#v, want nil", result.PartialOutput())
	}
}
