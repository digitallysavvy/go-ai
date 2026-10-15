package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func runBuiltinApproval(t *testing.T, call *ToolCallPart) (ai.LanguageModelCallEndEvent, []PendingToolApproval) {
	t.Helper()
	var mu sync.Mutex
	var lmEnd ai.LanguageModelCallEndEvent
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				call,
				&ToolApprovalRequestPart{ApprovalID: "a1", ToolCallID: call.ToolCallID},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
			}
		},
	})
	a, session := newTestAgent(t, mock, nil, Callbacks{
		OnLanguageModelCallEnd: func(_ context.Context, e ai.LanguageModelCallEndEvent) {
			mu.Lock()
			lmEnd = e
			mu.Unlock()
		},
	}, nil)
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{
		AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	approvals, _, _ := session.snapshotPendingState()
	mu.Lock()
	defer mu.Unlock()
	return lmEnd, approvals
}

// TS buildModelCallContent includes the step's approval requests.
func TestLanguageModelCallEnd_IncludesApprovalRequests(t *testing.T) {
	end, _ := runBuiltinApproval(t, &ToolCallPart{ToolCallID: "c1", ToolName: "Bash", Input: "{}", ProviderExecuted: true})
	var found bool
	for _, p := range end.Content {
		if ar, ok := p.(types.ToolApprovalRequestContent); ok && ar.ApprovalID == "a1" && ar.ToolCallID == "c1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("content lacks approval request: %+v", end.Content)
	}
}

// TS: pending builtin approval providerExecuted = rawToolCall.providerExecuted ?? true.
func TestBuiltinApproval_ProviderExecutedDefaultsTrueWhenOmitted(t *testing.T) {
	_, approvals := runBuiltinApproval(t, &ToolCallPart{ToolCallID: "c1", ToolName: "Bash", Input: "{}", ProviderExecutedOmitted: true})
	if len(approvals) != 1 || approvals[0].ProviderExecuted == nil || !*approvals[0].ProviderExecuted {
		t.Fatalf("approvals = %+v, want providerExecuted true", approvals)
	}
	_, approvals = runBuiltinApproval(t, &ToolCallPart{ToolCallID: "c1", ToolName: "Bash", Input: "{}"})
	if len(approvals) != 1 || approvals[0].ProviderExecuted == nil || *approvals[0].ProviderExecuted {
		t.Fatalf("explicit false must stay false: %+v", approvals)
	}
}

func TestDecodeStreamPart_ToolCallProviderExecutedOmitted(t *testing.T) {
	for in, want := range map[string]bool{
		`{"type":"tool-call","toolCallId":"c","toolName":"t","input":"{}"}`:                          true,
		`{"type":"tool-call","toolCallId":"c","toolName":"t","input":"{}","providerExecuted":false}`: false,
	} {
		p, err := DecodeStreamPart([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if got := p.(*ToolCallPart).ProviderExecutedOmitted; got != want {
			t.Fatalf("%s: omitted=%v want %v", in, got, want)
		}
	}
}
