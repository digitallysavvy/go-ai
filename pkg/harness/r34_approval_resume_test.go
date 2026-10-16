package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS sets `closingResumedStep = true` after a continuation settles an approval
// request the live stream re-emitted, so the finish-step closing that resumed
// step is discarded instead of reported as a completed step.
func TestRunPrompt_LiveApprovalContinuationClosesResumedStep(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: `{}`, ProviderExecuted: true, Dynamic: true},
				&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: "c1"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}},
	})
	chunks := drainRunPrompt(t, out)
	if n := len(chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)); n != 1 {
		t.Fatalf("approval responses = %d, want 1", n)
	}
	if steps := out.Result.Steps(); len(steps) != 0 {
		t.Fatalf("steps = %d, want 0 (the resumed step's finish-step is discarded)", len(steps))
	}
}
