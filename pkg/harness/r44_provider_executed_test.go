package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS records a custom (host tool) approval with `providerExecuted: false`.
// The record is persisted in lifecycle state, and when the approval continues,
// the rebuilt tool call copies the explicit false.
func TestR44CustomPendingApprovalRecordsProviderExecutedFalse(t *testing.T) {
	weather := types.Tool{
		Name:       "weather",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}
	tools := map[string]types.Tool{"weather": weather}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{}`},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	var mu sync.Mutex
	var recorded []PendingToolApproval
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval: ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
		OnPendingToolApproval: func(a PendingToolApproval) {
			mu.Lock()
			recorded = append(recorded, a)
			mu.Unlock()
		},
	})
	drainRunPrompt(t, out)
	<-out.Done
	mu.Lock()
	defer mu.Unlock()
	if len(recorded) != 1 {
		t.Fatalf("pending approvals = %+v, want 1", recorded)
	}
	a := recorded[0]
	if a.Kind != PendingToolApprovalCustom || a.ProviderExecuted == nil || *a.ProviderExecuted {
		t.Fatalf("pending approval = %+v, want kind custom with providerExecuted false", a)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"providerExecuted":false`) {
		t.Fatalf("pending approval JSON = %s, want providerExecuted:false", raw)
	}

	// Continuing the recorded approval: the approval response's tool call
	// keeps the explicit false.
	cont := newMockHarness(mockHarnessOptions{})
	out = runPrompt(context.Background(), runPromptInput{
		Harness: cont.harness, Session: cont.session, Mode: "continue", SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		PendingToolApprovals:      []PendingToolApproval{a},
		ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: a.ApprovalID, ToolCallID: "c1", Approved: true}},
	})
	chunks := drainRunPrompt(t, out)
	responses := chunksOfType(chunks, provider.ChunkTypeToolApprovalResponse)
	if len(responses) != 1 {
		t.Fatalf("approval responses = %+v, want 1", responses)
	}
	r := responses[0].ToolApprovalResponse
	if r.ProviderExecuted || r.ToolCall.ProviderExecuted || !r.ToolCall.ProviderExecutedSet {
		t.Fatalf("approval response = %+v, want tool call providerExecuted false (set)", r)
	}
}
