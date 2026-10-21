package harness

import (
	"context"
	"errors"
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

// TS lifecycle.error/abort start the lifecycle (onStart callback) and mark the
// turn ended even when no telemetry is configured.
func TestRunPrompt_StartFailureFiresOnStartWithoutTelemetry(t *testing.T) {
	sess := &mockSession{id: "r35-session"}
	sess.doPromptTurn = func(context.Context, PromptTurnOptions) (PromptControl, error) {
		return nil, errors.New("boom")
	}
	h := &mockHarnessAdapter{id: "mock", session: sess}
	var starts atomic.Int32
	out := runPrompt(context.Background(), runPromptInput{
		Harness: h, Session: sess,
		Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Callbacks: Callbacks{OnStart: func(context.Context, ai.OnStartEvent) { starts.Add(1) }},
	})
	drainRunPrompt(t, out)
	if starts.Load() != 1 {
		t.Fatalf("OnStart calls = %d, want 1", starts.Load())
	}
}

// TS lifecycle.languageModelCallEnd opens the step first: a finish-step with
// no content still reports step-start, model-call start and end.
func TestRunPrompt_ContentlessFinishStepStillOpensStep(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var starts, modelStarts, modelEnds, stepEnds atomic.Int32
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Callbacks: Callbacks{
			OnStepStart:              func(context.Context, ai.OnStepStartEvent) { starts.Add(1) },
			OnLanguageModelCallStart: func(context.Context, ai.LanguageModelCallStartEvent) { modelStarts.Add(1) },
			OnLanguageModelCallEnd:   func(context.Context, ai.LanguageModelCallEndEvent) { modelEnds.Add(1) },
			OnStepEnd:                func(context.Context, ai.OnStepFinishEvent) { stepEnds.Add(1) },
		},
	})
	drainRunPrompt(t, out)
	if starts.Load() != 1 || modelStarts.Load() != 1 || modelEnds.Load() != 1 || stepEnds.Load() != 1 {
		t.Fatalf("starts=%d modelStarts=%d modelEnds=%d stepEnds=%d, want 1 each", starts.Load(), modelStarts.Load(), modelEnds.Load(), stepEnds.Load())
	}
}
