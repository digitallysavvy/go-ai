package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS run-prompt.test.ts 'emits one error when the runtime echoes a failed
// approved custom tool result': the approved host tool fails, the runtime
// echoes the failed result back, and the stream carries exactly one
// tool-error for the call (not the echo as a second part).
func TestPort2carryApprovedFailedCustomToolEchoEmitsOneError(t *testing.T) {
	weather := types.Tool{
		Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("weather unavailable")
		},
	}
	tools := map[string]types.Tool{"weather": weather}
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&ToolResultPart{ToolCallID: "c1", ToolName: "weather", IsError: true, Result: map[string]interface{}{"error": "Error: weather unavailable"}},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue",
		Tools: tools, ActiveTools: tools, SandboxSession: testSandbox(),
		PendingToolApprovals: []PendingToolApproval{
			{ApprovalID: "approval-1", ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`, Kind: PendingToolApprovalCustom},
		},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "approval-1", ToolCallID: "c1", Approved: true}},
	})
	chunks := drainRunPrompt(t, out)
	results := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(results) != 1 || results[0].ToolResult.ToolCallID != "c1" || results[0].ToolResult.Error == nil ||
		!strings.Contains(results[0].ToolResult.Error.Error(), "weather unavailable") {
		t.Fatalf("tool results = %+v, want exactly one tool-error for c1 about the failure", results)
	}
}
