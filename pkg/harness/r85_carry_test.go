package harness

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r85carryBashTool() types.Tool {
	return types.Tool{Name: "bash", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}}}}
}

func r85carryTicketsTool(calls *int32, fails bool) types.Tool {
	return types.Tool{
		Name: "listProjectTickets", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(calls, 1)
			time.Sleep(10 * time.Millisecond)
			if fails {
				return nil, errors.New("Ticket lookup failed")
			}
			return []interface{}{map[string]interface{}{"id": "AI-23503"}}, nil
		},
	}
}

// Ports TS 6eab0f7b11 run-prompt.test.ts "pauses for repeated built-in
// approvals without a host result echo": a built-in approval blocks the
// runtime, so the echo of a host tool result submitted in the same message
// never arrives. The turn must still pause (it used to hang), report the
// host result as a completed pending result with the call's provider
// metadata, surface its outcome once, and on the next pause report the same
// pending result again without running the tool again.
func TestR85carryBuiltinApprovalPausesWithoutHostEcho(t *testing.T) {
	for _, tc := range []struct{ approved, fails bool }{{true, false}, {false, false}, {true, true}, {false, true}} {
		t.Run("", func(t *testing.T) {
			var calls int32
			bashCall := &ToolCallPart{ToolCallID: "bash-1", ToolName: "bash", NativeName: "Bash", Input: `{"command":"pwd"}`, ProviderExecuted: true}
			hostCall := &ToolCallPart{ToolCallID: "host-1", ToolName: "listProjectTickets", Input: `{}`, ProviderMetadata: ProviderMetadata{"test": {"source": "original-call"}}}
			approvalRequest := &ToolApprovalRequestPart{ApprovalID: "bash-1", ToolCallID: "bash-1"}
			var mu sync.Mutex
			var submitted []ToolResultSubmission
			record := func(s ToolResultSubmission) { mu.Lock(); submitted = append(submitted, s); mu.Unlock() }
			builtin := map[string]BuiltinTool{"bash": {Tool: r85carryBashTool(), NativeName: "Bash"}}
			// The runtime keeps the turn open while it waits for the
			// approval, as Claude Code does.
			hold := make(chan struct{})
			t.Cleanup(func() { close(hold) })
			holdTurn := func() <-chan struct{} { return hold }
			first := newMockHarness(mockHarnessOptions{
				builtinTools: builtin, supportsApproval: true, onSubmitResult: record, promptDone: holdTurn,
				script: func(func(string, interface{})) []StreamPart {
					return []StreamPart{&StreamStartPart{ModelID: "fake-model"}, bashCall, hostCall, approvalRequest, hostInputBoundary(7)}
				},
			})
			tickets := r85carryTicketsTool(&calls, tc.fails)
			tools := map[string]types.Tool{"bash": r85carryBashTool(), "listProjectTickets": tickets}
			active := map[string]types.Tool{"listProjectTickets": tickets}
			var pendingApprovals []PendingToolApproval
			var pendingResults []PendingToolResult
			out := runPrompt(context.Background(), runPromptInput{
				Harness: first.harness, Session: first.session, Prompt: TextPrompt("Run Bash and list the project tickets."), SandboxSession: testSandbox(),
				Tools: tools, ActiveTools: active,
				ToolApproval:          ToolApprovalConfiguration{"listProjectTickets": ai.ToolApprovalStatusApproved},
				OnPendingToolApproval: func(a PendingToolApproval) { pendingApprovals = append(pendingApprovals, a) },
				OnPendingToolResult:   func(r PendingToolResult) { pendingResults = append(pendingResults, r) },
			})
			done := make(chan []provider.StreamChunk)
			go func() { done <- drainRunPrompt(t, out) }()
			var chunks []provider.StreamChunk
			select {
			case chunks = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the turn never paused (stream hang)")
			}

			if n := atomic.LoadInt32(&calls); n != 1 {
				t.Fatalf("execute calls = %d, want 1", n)
			}
			mu.Lock()
			if len(submitted) != 1 {
				t.Fatalf("submitted = %d, want 1", len(submitted))
			}
			firstSubmission := submitted[0]
			mu.Unlock()
			if len(pendingApprovals) != 1 || pendingApprovals[0].ApprovalID != "bash-1" || pendingApprovals[0].Kind != PendingToolApprovalBuiltin {
				t.Fatalf("pending approvals = %+v", pendingApprovals)
			}
			if len(pendingResults) != 1 || pendingResults[0].ToolCallID != "host-1" || pendingResults[0].CompletedResult == nil ||
				!reflect.DeepEqual(pendingResults[0].CompletedResult.Output, firstSubmission.Output) ||
				!reflect.DeepEqual(pendingResults[0].ProviderOptions, map[string]map[string]any{"test": {"source": "original-call"}}) {
				t.Fatalf("pending results = %+v", pendingResults)
			}
			if n := len(chunksOfType(chunks, provider.ChunkTypeToolApprovalRequest)); n != 1 {
				t.Fatalf("approval requests = %d, want 1", n)
			}
			results := chunksOfType(chunks, provider.ChunkTypeToolResult)
			if len(results) != 1 || results[0].ToolResult.ToolCallID != "host-1" || (results[0].ToolResult.Error != nil) != tc.fails {
				t.Fatalf("tool results = %+v, want one host-1 (error %v)", results, tc.fails)
			}
			steps := out.Result.Steps()
			if len(steps) != 1 || steps[0].FinishReason != types.FinishReasonToolCalls {
				t.Fatalf("steps = %+v", steps)
			}
			if in := steps[0].Usage.InputTokens; in == nil || *in != 7 {
				t.Fatalf("usage = %+v, want 7", steps[0].Usage)
			}
			wantContent := []string{"tool-call", "tool-call", "tool-approval-request", "tool-result"}
			if tc.fails {
				wantContent[3] = "tool-error"
			}
			if got := contentTypes(steps[0]); !reflect.DeepEqual(got, wantContent) {
				t.Fatalf("content = %v, want %v", got, wantContent)
			}

			// Second slice: the bash-1 answer is carried out, the runtime asks
			// for another built-in approval, and host-1 is still not echoed.
			second := newMockHarness(mockHarnessOptions{
				builtinTools: builtin, supportsApproval: true, onSubmitResult: record, promptDone: holdTurn,
				continueScript: func(func(string, interface{})) []StreamPart {
					bash2 := *bashCall
					bash2.ToolCallID = "bash-2"
					return []StreamPart{&bash2, &ToolApprovalRequestPart{ApprovalID: "bash-2", ToolCallID: "bash-2"}, hostInputBoundary(3)}
				},
			})
			var nextApprovals []PendingToolApproval
			var nextResults []PendingToolResult
			out2 := runPrompt(context.Background(), runPromptInput{
				Harness: second.harness, Session: second.session, Mode: "continue", SandboxSession: testSandbox(),
				Tools: tools, ActiveTools: active,
				PendingToolApprovals:      pendingApprovals,
				PendingToolResults:        pendingResults,
				ToolApprovalContinuations: []types.ToolApprovalResponseContent{{ApprovalID: "bash-1", ToolCallID: "bash-1", Approved: tc.approved}},
				OnPendingToolApproval:     func(a PendingToolApproval) { nextApprovals = append(nextApprovals, a) },
				OnPendingToolResult:       func(r PendingToolResult) { nextResults = append(nextResults, r) },
			})
			go func() { done <- drainRunPrompt(t, out2) }()
			var chunks2 []provider.StreamChunk
			select {
			case chunks2 = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the continued turn never paused")
			}
			if !reflect.DeepEqual(nextResults, pendingResults) {
				t.Fatalf("next pending results = %+v, want the same as %+v", nextResults, pendingResults)
			}
			if len(nextApprovals) != 1 || nextApprovals[0].ApprovalID != "bash-2" || nextApprovals[0].Kind != PendingToolApprovalBuiltin {
				t.Fatalf("next approvals = %+v", nextApprovals)
			}
			for _, c := range chunks2 {
				if (c.ToolCall != nil && c.ToolCall.ID == "host-1") || (c.ToolResult != nil && c.ToolResult.ToolCallID == "host-1") {
					t.Fatalf("host-1 surfaced again: %+v", c)
				}
			}
			if n := atomic.LoadInt32(&calls); n != 1 {
				t.Fatalf("execute calls = %d, want 1", n)
			}
			mu.Lock()
			n := len(submitted)
			mu.Unlock()
			if n != 2 {
				t.Fatalf("submitted = %d, want 2 (the completed result resubmitted)", n)
			}
		})
	}
}
