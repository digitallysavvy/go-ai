package harness

import (
	"context"
	"errors"
	"testing"
)

// TS settleLifecycle calls lifecycle.abort(abortSignal.reason): the abort
// event's reason is the caller's cancellation cause, even when a harness
// error part is what ended the turn.
func TestR47TelemetryAbortReasonIsCancellationCause(t *testing.T) {
	rec := &recordingIntegration{}
	cause := errors.New("user pressed stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	runFailingTurn(t, ctx, rec, "bridge failed")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.aborts) != 1 {
		t.Fatalf("aborts = %d, want 1", len(rec.aborts))
	}
	if !errors.Is(rec.aborts[0].Reason, cause) {
		t.Fatalf("abort Reason = %v, want the cancellation cause", rec.aborts[0].Reason)
	}
}
