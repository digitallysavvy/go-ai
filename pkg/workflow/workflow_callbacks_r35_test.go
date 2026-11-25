package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS mergeCallbacks uses Promise.allSettled: a callback that throws neither
// stops the other callbacks nor reaches the caller (here, approval resume and
// the error/abort paths call the merged callbacks directly).
func TestMergedCallbacksSwallowPanicsAndRunAll(t *testing.T) {
	var ran []string
	toolStart := mergeToolStart(
		func(context.Context, ai.OnToolCallStartEvent) { ran = append(ran, "a"); panic("boom") },
		func(context.Context, ai.OnToolCallStartEvent) { ran = append(ran, "b") },
	)
	toolStart(context.Background(), ai.OnToolCallStartEvent{})
	if len(ran) != 2 {
		t.Fatalf("ran = %v, want both callbacks", ran)
	}

	// A single callback is guarded as well.
	only := mergeToolEnd(func(context.Context, ai.OnToolCallFinishEvent) { panic("boom") }, nil)
	only(context.Background(), ai.OnToolCallFinishEvent{})

	got := 0
	onErr := mergeError(func(context.Context, error) { panic("boom") }, func(context.Context, error) { got++ })
	onErr(context.Background(), errors.New("x"))
	onAbort := mergeAbort(func(context.Context, []types.StepResult) { panic("boom") }, func(context.Context, []types.StepResult) { got++ })
	onAbort(context.Background(), nil)
	if got != 2 {
		t.Fatalf("second callbacks ran %d times, want 2", got)
	}
	if mergeStart(nil, nil) != nil || mergeError(nil, nil) != nil || mergeAbort(nil, nil) != nil {
		t.Fatal("two nil callbacks must stay nil")
	}
}
