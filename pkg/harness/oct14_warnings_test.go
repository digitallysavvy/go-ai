package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports TS #22105 "preserves adapter warnings on the step and aggregate
// result".
func TestRunPrompt_PreservesAdapterWarnings(t *testing.T) {
	warnings := []CallWarning{
		{Type: CallWarningUnsupportedSetting, Setting: "temperature", Details: "The adapter does not support temperature."},
		{Type: CallWarningUnsupportedTool, Tool: "web_search", Details: "The adapter does not support web search."},
		{Type: CallWarningOther, Message: "The adapter used a fallback."},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{Warnings: warnings},
				&TextStartPart{ID: "t1"},
				&TextDeltaPart{ID: "t1", Delta: "done"},
				&TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
	})
	chunks := drainRunPrompt(t, out)

	// The public full stream carries no stream-start marker; the warnings
	// live on the step and the aggregate result below.
	if starts := chunksOfType(chunks, provider.ChunkTypeStreamStart); len(starts) != 0 {
		t.Fatalf("public stream exposes stream-start markers: %+v", starts)
	}
	steps := out.Result.Steps()
	if len(steps) != 1 || len(steps[0].Warnings) != 3 {
		t.Fatalf("steps = %+v, want one step with 3 warnings", steps)
	}
	agg := out.Result.Warnings()
	if len(agg) != 3 {
		t.Fatalf("Warnings() = %+v; want 3", agg)
	}
	if agg[1].Type != "unsupported-tool" || agg[2].Message != "The adapter used a fallback." {
		t.Fatalf("warnings = %+v", agg)
	}
}
