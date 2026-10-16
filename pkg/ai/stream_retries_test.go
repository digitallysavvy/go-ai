package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestStreamText_StreamRetriesRecoversFromMidStreamError verifies that a
// retryable mid-stream provider error (a ChunkTypeError chunk whose message
// normalizes to a retryable providererrors.StreamProviderError) reopens the
// model call when StreamRetries permits it, and that the recovered step
// reflects only the successful attempt's text (audit row 802af1e / WG8).
func TestStreamText_StreamRetriesRecoversFromMidStreamError(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "partial attempt "},
					// "Internal Server Error" is one of the exact messages
					// NormalizeStreamProviderError infers as retryable.
					{Type: provider.ChunkTypeError, Text: "Internal Server Error"},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "success"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var reportedErrors []error
	streamRetries := 1
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "hi",
		StreamRetries: &streamRetries,
		OnError: func(_ context.Context, e error) {
			reportedErrors = append(reportedErrors, e)
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected the model to be called twice (initial + 1 retry), got %d", callCount)
	}
	if text != "success" {
		t.Fatalf("Text() = %q, want %q (the recovered step must reflect only the successful attempt)", text, "success")
	}
	if len(reportedErrors) != 1 {
		t.Fatalf("expected OnError to fire once (for the retried error), got %d: %v", len(reportedErrors), reportedErrors)
	}
	if !providererrors.IsStreamProviderError(reportedErrors[0]) {
		t.Fatalf("expected OnError to receive a normalized *StreamProviderError, got %T: %v", reportedErrors[0], reportedErrors[0])
	}
	if result.FinishReason() != types.FinishReasonStop {
		t.Fatalf("FinishReason() = %v, want %v", result.FinishReason(), types.FinishReasonStop)
	}
}

// TestStreamText_StreamRetriesSkipsToolChoiceViolation verifies that a
// mid-stream error which is a ToolChoiceViolationError is never retried,
// even when StreamRetries would otherwise permit it (TS
// ToolChoiceViolationError.isInstance skip check).
func TestStreamText_StreamRetriesSkipsToolChoiceViolation(t *testing.T) {
	t.Parallel()

	violation := &ToolChoiceViolationError{
		ToolChoice:   types.ToolChoice{Type: types.ToolChoiceRequired},
		FinishReason: types.FinishReasonStop,
	}
	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeError, Err: violation, Text: violation.Error()},
			}), nil
		},
	}

	streamRetries := 5
	var reportedErrors []error
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "hi",
		StreamRetries: &streamRetries,
		OnError: func(_ context.Context, e error) {
			reportedErrors = append(reportedErrors, e)
		},
		OnErrorRetry: func(context.Context, StreamTextOnErrorRetryEvent) bool {
			return true // would request a retry if ever consulted
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, _ = result.ReadAll()

	if callCount != 1 {
		t.Fatalf("expected exactly 1 model call (no retry for a ToolChoiceViolationError), got %d", callCount)
	}
	// Non-fatal error chunks (retried or not) don't set a fatal
	// StreamTextResult.Err(); they're reported via OnError and reflected in
	// FinishReason, matching how a plain unretried ChunkTypeError behaves.
	if len(reportedErrors) != 1 || !IsToolChoiceViolationError(reportedErrors[0]) {
		t.Fatalf("expected OnError to receive the unretried, unnormalized ToolChoiceViolationError, got %v", reportedErrors)
	}
	if result.FinishReason() != types.FinishReasonError {
		t.Fatalf("FinishReason() = %v, want %v", result.FinishReason(), types.FinishReasonError)
	}
}

// TestStreamText_StreamRetriesAutomaticRetryIgnoresIsRetryable verifies that
// the automatic streamRetries budget retries ANY mid-stream error chunk
// while budget remains, not only ones StreamProviderError classifies as
// IsRetryable=true (TS stream-text.ts's `automaticRetry =
// !isToolChoiceViolation && automaticStreamRetryCount < streamRetries` has
// no isRetryable check; ported from stream-text.test.ts's "should support
// mid-stream provider error recovery" style cases using a plain, unclassified
// error message).
func TestStreamText_StreamRetriesAutomaticRetryIgnoresIsRetryable(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "partial "},
					// A generic message that does NOT match any of the
					// exact-message retryable inferences and carries no
					// status code, so NormalizeStreamProviderError produces
					// IsRetryable=false. TS still retries it automatically.
					{Type: provider.ChunkTypeError, Text: "provider error"},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "success"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	streamRetries := 1
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "hi",
		StreamRetries: &streamRetries,
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	text, err := result.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected the model to be called twice (initial + 1 automatic retry despite IsRetryable=false), got %d", callCount)
	}
	if text != "success" {
		t.Fatalf("Text() = %q, want %q", text, "success")
	}
	if result.FinishReason() != types.FinishReasonStop {
		t.Fatalf("FinishReason() = %v, want %v", result.FinishReason(), types.FinishReasonStop)
	}
}

// TestStreamText_OnErrorRetryRequiresExplicitStreamRetries verifies that
// OnErrorRetry is never consulted when StreamRetries is nil (omitted
// entirely), matching TS's canRetryStreamViaOnError = streamRetries !==
// undefined && onErrorArg != null: omitting the option disables ALL stream
// retry behavior, even a callback-directed one.
func TestStreamText_OnErrorRetryRequiresExplicitStreamRetries(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "partial "},
				{Type: provider.ChunkTypeError, Text: "Internal Server Error"},
			}), nil
		},
	}

	onErrorRetryCalled := false
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		// StreamRetries intentionally omitted (nil).
		OnErrorRetry: func(context.Context, StreamTextOnErrorRetryEvent) bool {
			onErrorRetryCalled = true
			return true
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_, _ = result.ReadAll()

	if callCount != 1 {
		t.Fatalf("expected exactly 1 model call (OnErrorRetry must not be consulted when StreamRetries is nil), got %d", callCount)
	}
	if onErrorRetryCalled {
		t.Fatalf("expected OnErrorRetry not to be called when StreamRetries is nil")
	}
}

// TestStreamText_StreamRetriesRejectsNegativeValue verifies that a negative
// StreamRetries is rejected synchronously (TS stream-text.test.ts "should
// reject invalid streamRetries values").
func TestStreamText_StreamRetriesRejectsNegativeValue(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			t.Fatal("model should not be called when streamRetries is invalid")
			return nil, nil
		},
	}

	negative := -1
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "hi",
		StreamRetries: &negative,
	})
	if err == nil {
		t.Fatal("expected an error for negative StreamRetries, got nil")
	}
	var target *providererrors.InvalidArgumentError
	if !errors.As(err, &target) {
		t.Fatalf("expected *providererrors.InvalidArgumentError, got %T: %v", err, err)
	}
}

func collectChunkTypes(t *testing.T, result *StreamTextResult) []provider.ChunkType {
	t.Helper()
	var got []provider.ChunkType
	stream := result.NewStream()
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		got = append(got, c.Type)
	}
	return got
}

// A failed attempt's tool parts (and everything after them) are held back and
// dropped on retry: the consumer never sees them and the tool never runs
// (TS stream-text.ts bufferedAttemptParts). The text block the attempt left
// open is closed.
func TestStreamText_StreamRetriesDropsBufferedToolParts(t *testing.T) {
	t.Parallel()

	var executed int
	approvalCalls := 0
	tools := []types.Tool{{
		Name: "search",
		ToolApproval: types.ToolNeedsApprovalFunc(func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool {
			approvalCalls++
			return false
		}),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed++
			return "ok", nil
		},
	}}
	call := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			call++
			if call == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "t1"},
					{Type: provider.ChunkTypeText, ID: "t1", Text: "thinking"},
					{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c1", ToolName: "search"}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "search", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeError, Text: "Internal Server Error"},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	retries := 1
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "hi", Tools: tools, StreamRetries: &retries,
	})
	if err != nil {
		t.Fatal(err)
	}
	types1 := collectChunkTypes(t, result)
	if call != 2 || executed != 0 || approvalCalls != 0 {
		t.Fatalf("calls=%d executed=%d approvals=%d", call, executed, approvalCalls)
	}
	for _, ct := range types1 {
		if ct == provider.ChunkTypeToolInputStart || ct == provider.ChunkTypeToolCall || ct == provider.ChunkTypeToolResult {
			t.Fatalf("discarded attempt leaked %s: %v", ct, types1)
		}
	}
	var sawTextEnd bool
	for _, ct := range types1 {
		if ct == provider.ChunkTypeTextEnd {
			sawTextEnd = true
		}
	}
	if !sawTextEnd {
		t.Fatalf("open text block of the failed attempt was not closed: %v", types1)
	}
	if got := result.Text(); got != "done" {
		t.Fatalf("Text() = %q", got)
	}
}

// With the retry budget exhausted the held parts are delivered, in order,
// before the error.
func TestStreamText_StreamRetriesFlushesBufferedPartsOnTerminalError(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "search", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeText, Text: "after"},
				{Type: provider.ChunkTypeError, Text: "boom"},
			}), nil
		},
	}
	retries := 0
	var reported int
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "hi", StreamRetries: &retries,
		OnErrorRetry: func(context.Context, StreamTextOnErrorRetryEvent) bool { return false },
		OnError:      func(context.Context, error) { reported++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	got := collectChunkTypes(t, result)
	idx := map[provider.ChunkType]int{}
	for i, ct := range got {
		if _, ok := idx[ct]; !ok {
			idx[ct] = i
		}
	}
	tc, okc := idx[provider.ChunkTypeToolCall]
	tx, okt := idx[provider.ChunkTypeText]
	er, oke := idx[provider.ChunkTypeError]
	if !okc || !okt || !oke || tc >= tx || tx >= er {
		t.Fatalf("want tool-call < text < error, got %v", got)
	}
	if reported != 1 {
		t.Fatalf("OnError calls = %d, want 1", reported)
	}
}

// TestStreamText_StreamRetriesToolInputCallbacksUseExecutionTools verifies
// that after a streamRetries reopen the tool-input callbacks are rebuilt from
// the execution tools (TS invokeToolCallbacksFromStream receives the same
// tools on every attempt), so a tool hidden from the model by a tool caller
// still gets OnInputStart on the retried attempt.
func TestStreamText_StreamRetriesToolInputCallbacksUseExecutionTools(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			start := provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: "c1", ToolCall: &types.ToolCall{ID: "c1", ToolName: "hidden"}}
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					start,
					{Type: provider.ChunkTypeError, Text: "Internal Server Error"},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				start,
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	var mu sync.Mutex
	starts := 0
	streamRetries := 1
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:         model,
		Prompt:        "hi",
		StreamRetries: &streamRetries,
		Tools: []types.Tool{
			localCallerTool("code_mode", nil),
			{
				Name:       "hidden",
				Parameters: map[string]interface{}{"type": "object"},
				OnInputStart: func(context.Context, types.OnInputStartOptions) error {
					mu.Lock()
					starts++
					mu.Unlock()
					return nil
				},
			},
		},
		ExperimentalToolCallers: ExperimentalToolCallers{"hidden": {"code_mode"}},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callCount != 2 || starts != 1 {
		t.Fatalf("callCount=%d starts=%d, want 2 and 1 (the dropped first attempt is held back; the retried attempt must reach the hidden tool)", callCount, starts)
	}
}
