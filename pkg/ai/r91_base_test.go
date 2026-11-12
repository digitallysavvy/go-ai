package ai

import (
	"context"
	"errors"
	"testing"
	"time"
)

// B4 (core1-A01): NewToolTimeoutError builds the tool timeout error with
// its configured duration marked as set; a zero duration has the TS text
// (core1-A01, AbortSignal.timeout(0)).
func TestR91baseNewToolTimeoutError(t *testing.T) {
	e := NewToolTimeoutError(50*time.Millisecond, context.DeadlineExceeded)
	if e.Reason != TimeoutReasonTool || e.Timeout != 50*time.Millisecond || !e.timeoutSet {
		t.Fatalf("got %+v", e)
	}
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("cause not wrapped")
	}
	if e.Error() != "The operation was aborted due to timeout" {
		t.Fatalf("text = %q", e.Error())
	}
	zero := NewToolTimeoutError(0, context.DeadlineExceeded)
	if !zero.timeoutSet || zero.Error() != "The operation was aborted due to timeout" {
		t.Fatalf("zero-duration text = %q", zero.Error())
	}

	d := time.Duration(0)
	tc := &TimeoutConfig{PerChunk: &d}
	got := tc.timeoutError(TimeoutReasonChunk, context.DeadlineExceeded).(*TimeoutError)
	if !got.timeoutSet || got.Timeout != 0 {
		t.Fatalf("configured zero chunk timeout not marked set: %+v", got)
	}
	if got := tc.timeoutError(TimeoutReasonStep, context.DeadlineExceeded).(*TimeoutError); got.timeoutSet {
		t.Fatal("unconfigured step timeout marked set")
	}
}
