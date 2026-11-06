package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS enqueueAutomaticApprovalResponse sends `providerExecuted: false` on the
// automatic denial of a custom tool.
func TestR44CustomToolDenialResponseHasExplicitProviderExecutedFalse(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "ask", Input: "{}"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	ask := types.Tool{Name: "ask", Parameters: map[string]interface{}{"type": "object"}}
	tools := map[string]types.Tool{"ask": ask}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval: ToolApprovalConfiguration{"ask": ai.ToolApprovalStatusDenied},
	})
	chunks := drainRunPrompt(t, out)
	responses := chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)
	if len(responses) != 1 {
		t.Fatalf("approval responses = %+v, want 1", responses)
	}
	if r := responses[0].ToolApprovalResponse; r.ProviderExecuted || !r.ProviderExecutedSet {
		t.Fatalf("approval response = %+v, want explicit providerExecuted false", r)
	}
}

// TS records a custom approval with `providerExecuted: false`, and
// enqueueApprovalResponse spreads it into the continuation's response.
func TestR44CustomApprovalRecordAndContinuationHaveExplicitProviderExecutedFalse(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "ask", Input: "{}"},
			}
		},
	})
	ask := types.Tool{Name: "ask", Parameters: map[string]interface{}{"type": "object"}}
	tools := map[string]types.Tool{"ask": ask}
	var pending []PendingToolApproval
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval:          ToolApprovalConfiguration{"ask": ai.ToolApprovalStatusUserApproval},
		OnPendingToolApproval: func(p PendingToolApproval) { pending = append(pending, p) },
	})
	drainRunPrompt(t, out)
	if len(pending) != 1 || pending[0].ProviderExecuted == nil || *pending[0].ProviderExecuted {
		t.Fatalf("pending approvals = %+v, want providerExecuted false", pending)
	}

	mock2 := newMockHarness(mockHarnessOptions{})
	out = runPrompt(context.Background(), runPromptInput{
		Harness: mock2.harness, Session: mock2.session, Mode: "continue", SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		PendingToolApprovals:      pending,
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: pending[0].ApprovalID, ToolCallID: "c1", Approved: false}},
	})
	responses := chunksOfType(drainRunPrompt(t, out), provider.ChunkTypeToolApprovalResponse)
	if len(responses) != 1 {
		t.Fatalf("approval responses = %+v, want 1", responses)
	}
	if r := responses[0].ToolApprovalResponse; r.ProviderExecuted || !r.ProviderExecutedSet {
		t.Fatalf("approval response = %+v, want explicit providerExecuted false", r)
	}
}

// TS translateStreamPart copies the originating call's providerExecuted onto
// the tool-result when it is not undefined, so an explicit false is kept.
func TestR44TranslatedToolResultKeepsExplicitProviderExecutedFalse(t *testing.T) {
	for name, tc := range map[string]struct {
		info    ToolCallInfo
		wantSet bool
	}{
		"explicit false": {ToolCallInfo{ProviderExecutedSet: true}, true},
		"undefined":      {ToolCallInfo{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			chunks := TranslatePart(&ToolResultPart{ToolCallID: "c1", ToolName: "echo", Result: "ok"}, TranslateOptions{
				GetToolCall: func(string) (ToolCallInfo, bool) { return tc.info, true },
			})
			if len(chunks) != 1 || chunks[0].ToolResult == nil {
				t.Fatalf("chunks = %+v", chunks)
			}
			if r := chunks[0].ToolResult; r.ProviderExecuted || r.ProviderExecutedSet != tc.wantSet {
				t.Fatalf("tool result = %+v, want providerExecuted false with set=%v", r, tc.wantSet)
			}
		})
	}
}
