package ai

import (
	"context"
	"errors"
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
