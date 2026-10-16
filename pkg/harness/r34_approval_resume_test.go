package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
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

// TS processPendingApprovalContinuation starts the turn lifecycle and publishes
// the tool execution start for an approved host tool before it finds the tool
// has no execute function and pauses for the client result.
func TestRunPrompt_ApprovedClientToolContinuationStartsLifecycle(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{})
	clientTool := types.Tool{Name: "ask", Parameters: map[string]interface{}{"type": "object"}}
	tools := map[string]types.Tool{"ask": clientTool}
	var starts, toolStarts int
	var pendingResults []PendingToolResult
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue", SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		PendingToolApprovals:      []PendingToolApproval{{ApprovalID: "a1", ToolCallID: "c1", ToolName: "ask", Input: `{}`, Kind: PendingToolApprovalCustom}},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "a1", ToolCallID: "c1", Approved: true}},
		OnPendingToolResult:       func(r PendingToolResult) { pendingResults = append(pendingResults, r) },
		Callbacks: Callbacks{
			OnStart:              func(context.Context, ai.OnStartEvent) { starts++ },
			OnToolExecutionStart: func(context.Context, ai.OnToolCallStartEvent) { toolStarts++ },
		},
	})
	drainRunPrompt(t, out)
	if len(pendingResults) != 1 {
		t.Fatalf("pending results = %d, want 1", len(pendingResults))
	}
	if starts != 1 || toolStarts != 1 {
		t.Fatalf("OnStart=%d OnToolExecutionStart=%d, want 1 and 1", starts, toolStarts)
	}
}

// TS lifecycle.end starts the lifecycle (onStart) before it notices the turn
// has no steps, so a turn that only ever sends `finish` still reports onStart.
func TestRunPrompt_FinishOnlyTurnStillFiresOnStart(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)}}
		},
	})
	var starts, ends int
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Callbacks: Callbacks{
			OnStart: func(context.Context, ai.OnStartEvent) { starts++ },
			OnEnd:   func(context.Context, ai.OnFinishEvent) { ends++ },
		},
	})
	drainRunPrompt(t, out)
	if starts != 1 || ends != 0 {
		t.Fatalf("OnStart=%d OnEnd=%d, want 1 and 0", starts, ends)
	}
}
