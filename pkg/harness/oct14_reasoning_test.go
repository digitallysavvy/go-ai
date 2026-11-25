package harness

import (
	"context"
	"testing"
)

// Ports TS #23095: top-level reasoning accessors come from the final step.
func TestRunPrompt_ReasoningAccessorsExposeFinalStep(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ReasoningStartPart{ID: "r1"}, &ReasoningDeltaPart{ID: "r1", Delta: "First thought."}, &ReasoningEndPart{ID: "r1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&ReasoningStartPart{ID: "r2"}, &ReasoningDeltaPart{ID: "r2", Delta: "Final thought."}, &ReasoningEndPart{ID: "r2"},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "Answer."}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
	})
	drainRunPrompt(t, out)
	r := out.Result

	steps := r.Steps()
	if len(steps) != 2 || steps[0].ReasoningText != "First thought." || steps[1].ReasoningText != "Final thought." {
		t.Fatalf("steps reasoning = %+v", steps)
	}
	if got := r.ReasoningText(); got != "Final thought." { //nolint:staticcheck
		t.Fatalf("ReasoningText = %q, want final step's", got)
	}
	rs := r.Reasoning() //nolint:staticcheck
	if len(rs) != 1 || rs[0].Text != "Final thought." {
		t.Fatalf("Reasoning = %+v", rs)
	}
	if fs := r.FinalStep(); fs.ReasoningText != "Final thought." {
		t.Fatalf("FinalStep.ReasoningText = %q", fs.ReasoningText)
	}
}

func TestRunPrompt_ReasoningAccessorsEmptyWithoutReasoning(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "Answer."}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
	})
	drainRunPrompt(t, out)
	if len(out.Result.Reasoning()) != 0 || out.Result.ReasoningText() != "" { //nolint:staticcheck
		t.Fatalf("expected empty reasoning accessors")
	}
}
