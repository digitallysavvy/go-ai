package workflow

import (
	"context"
	"errors"
	"testing"
)

// TestR94integPrecancelledAbortReason: TS WorkflowAgent
// options.abortSignal?.throwIfAborted() throws signal.reason.
func TestR94integPrecancelledAbortReason(t *testing.T) {
	reason := errors.New("r94integ: caller reason")
	for _, stream := range []bool{false, true} {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(reason)
		wa, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			_, err = wa.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi"})
		} else {
			_, err = wa.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi"})
		}
		if !errors.Is(err, reason) {
			t.Errorf("stream=%v: err = %v, want the abort reason", stream, err)
		}
	}
}
