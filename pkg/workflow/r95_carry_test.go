package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r95-gen-to-carry: TS mapWithConcurrency (workflow-agent.ts 2049) checks
// abortSignal.throwIfAborted() before each claim when the limit is below the
// approved call count, so an abort during the resumed calls rejects the run
// with the reason before onStart / onAbort. Without a limit (Promise.all) the
// resume finishes and the run reports its start, then the abort.
func TestR95carryApprovalResumeAbort(t *testing.T) {
	errReason := errors.New("r95carry abort reason")
	for _, tc := range []struct {
		name      string
		limit     *int
		wantAbort bool
	}{{"limit 1", r74carryIntPtr(1), false}, {"no limit", nil, true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			var executed, aborted atomic.Int32
			agent, err := NewWorkflowAgent(WorkflowAgent{
				Model:               &wfMockModel{},
				ToolCallConcurrency: tc.limit,
				Tools: []types.Tool{{
					Name:         "tool1",
					ToolApproval: true,
					Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
						executed.Add(1)
						cancel(errReason)
						return "ok", nil
					},
				}},
				OnAbort: func(context.Context, []types.StepResult) { aborted.Add(1) },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = agent.GenerateWithOptions(ctx, WorkflowGenerateOptions{Messages: r74carryApprovedMessages("call-1", "call-2")})
			if !errors.Is(err, errReason) {
				t.Fatalf("err = %v, want the abort reason", err)
			}
			if got := aborted.Load() == 1; got != tc.wantAbort {
				t.Errorf("onAbort called = %v, want %v", got, tc.wantAbort)
			}
			if tc.limit != nil && executed.Load() != 1 {
				t.Errorf("executed %d calls, want 1", executed.Load())
			}
		})
	}
}
