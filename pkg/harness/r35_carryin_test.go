package harness

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS run-prompt.ts opens the step (lifecycle.ensureStepOpen) before the
// settled-replay `continue`, so a step whose only content is a replay of an
// already-settled call still opens (and closes) as a step.
func TestRunPrompt_SettledReplayOpensStep(t *testing.T) {
	restricted := types.Tool{Name: "restricted", Parameters: restrictedSchema()}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "restricted", Input: `{"city":`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				// Second step: only a replay of the settled call.
				&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	tools := map[string]types.Tool{"restricted": restricted}
	var starts atomic.Int32
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(),
		Callbacks:      Callbacks{OnStepStart: func(context.Context, ai.OnStepStartEvent) { starts.Add(1) }},
	})
	drainRunPrompt(t, out)
	if got := starts.Load(); got != 2 {
		t.Fatalf("OnStepStart calls = %d, want 2", got)
	}
}

// TS: a terminal finish with an unclosed step calls onTurnFinished, then
// result.finish() rejects and the catch calls onTurnFailed.
func TestRunPrompt_FinishWithUnclosedStepFiresFinishedAndFailed(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextDeltaPart{ID: "t1", Delta: "x"},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var finished, failed atomic.Int32
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		OnTurnFinished: func() { finished.Add(1) },
		OnTurnFailed:   func() { failed.Add(1) },
	})
	drainRunPrompt(t, out)
	if finished.Load() != 1 || failed.Load() != 1 {
		t.Fatalf("finished=%d failed=%d, want 1 and 1", finished.Load(), failed.Load())
	}
}
