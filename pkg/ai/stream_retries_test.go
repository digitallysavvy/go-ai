package ai

import (
	"context"
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
