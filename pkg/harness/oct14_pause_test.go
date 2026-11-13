package harness

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func weatherTool() types.Tool {
	return types.Tool{
		Name: "weather", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]any{"temperature": 72}, nil
		},
	}
}

func hostInputBoundary(total int) StreamPart {
	return &FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "host-input"}, Usage: stringUsage(total, 0)}
}

func contentTypes(step types.StepResult) []string {
	var out []string
	for _, p := range step.Content {
		out = append(out, p.ContentType())
	}
	return out
}

// Ports TS #23260 "surfaces the rest of the step and pauses at the next
// finish-step".
func TestRunPrompt_HostInputPauseSurfacesRestOfStep(t *testing.T) {
	var pending []PendingToolApproval
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`},
				&ToolCallPart{ToolCallID: "native-1", ToolName: "read", Input: `{"path":"a.txt"}`, ProviderExecuted: true, Dynamic: true},
				&ToolResultPart{ToolCallID: "native-1", ToolName: "read", Result: "file contents"},
				&ToolCallPart{ToolCallID: "c2", ToolName: "weather", Input: `{"city":"NYC"}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "host-input"}, Usage: stringUsage(7, 0)},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "must not appear"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tools := map[string]types.Tool{"weather": weatherTool()}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval:          ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
		OnPendingToolApproval: func(a PendingToolApproval) { pending = append(pending, a) },
	})
	chunks := drainRunPrompt(t, out)

	if len(pending) != 2 || pending[0].ToolCallID != "c1" || pending[1].ToolCallID != "c2" {
		t.Fatalf("pending = %+v, want c1 then c2", pending)
	}
	if n := len(chunksOfType(chunks, provider.ChunkTypeText)); n != 0 {
		t.Fatalf("text chunks = %d, want 0", n)
	}
	steps := out.Result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finishReason = %s", steps[0].FinishReason)
	}
	if in := steps[0].Usage.InputTokens; in == nil || *in != 7 {
		t.Fatalf("usage = %+v, want boundary usage 7", steps[0].Usage)
	}
	want := []string{"tool-call", "tool-approval-request", "tool-call", "tool-result", "tool-call", "tool-approval-request"}
	got := contentTypes(steps[0])
	if len(got) != len(want) {
		t.Fatalf("content = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("content = %v, want %v", got, want)
		}
	}
}

// Ports "ignores a finish-step while a host tool has not been echoed yet".
func TestRunPrompt_FinishStepIgnoredUntilHostToolEchoed(t *testing.T) {
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`})
			emit(hostInputBoundary(3))
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: "weather", Result: sub.Output})
			emit(hostInputBoundary(5))
			emit(&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(5, 0)})
		},
	)
	tools := map[string]types.Tool{"weather": weatherTool()}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	drainRunPrompt(t, out)

	steps := out.Result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if in := steps[0].Usage.InputTokens; in == nil || *in != 5 {
		t.Fatalf("usage = %+v, want the second boundary's (5)", steps[0].Usage)
	}
	got := contentTypes(steps[0])
	if len(got) != 2 || got[0] != "tool-call" || got[1] != "tool-result" {
		t.Fatalf("content = %v", got)
	}
}

// Ports "waits for the echo of host tool results before pausing".
func TestRunPrompt_PauseWaitsForHostToolEcho(t *testing.T) {
	local := weatherTool()
	local.Name = "local"
	local.Execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return map[string]any{"local": true}, nil
	}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "local-1", ToolName: "local", Input: `{"city":"SF"}`})
			emit(&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`})
			emit(hostInputBoundary(2))
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			go func() {
				time.Sleep(10 * time.Millisecond)
				emit(&ToolResultPart{ToolCallID: sub.ToolCallID, ToolName: "local", Result: sub.Output})
			}()
		},
	)
	tools := map[string]types.Tool{"weather": weatherTool(), "local": local}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
		ToolApproval: ToolApprovalConfiguration{"weather": ai.ToolApprovalStatusUserApproval},
	})
	chunks := drainRunPrompt(t, out)

	results := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(results) != 1 || results[0].ToolResult.ToolCallID != "local-1" {
		t.Fatalf("tool results = %+v, want only local-1", results)
	}
	steps := out.Result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if in := steps[0].Usage.InputTokens; in == nil || *in != 2 {
		t.Fatalf("usage = %+v", steps[0].Usage)
	}
	want := []string{"tool-call", "tool-call", "tool-approval-request", "tool-result"}
	got := contentTypes(steps[0])
	if len(got) != len(want) {
		t.Fatalf("content = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("content = %v, want %v", got, want)
		}
	}
}

// Ports "closes a step whose finish-step was ignored when the turn finishes".
func TestRunPrompt_IgnoredFinishStepClosedAtTurnFinish(t *testing.T) {
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "weather", Input: `{"city":"SF"}`},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tools := map[string]types.Tool{"weather": weatherTool()}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	drainRunPrompt(t, out)
	if err := out.Result.Err(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	steps := out.Result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if got := contentTypes(steps[0]); len(got) != 1 || got[0] != "tool-call" {
		t.Fatalf("content = %v", got)
	}
	if out.Result.FinishReason() != types.FinishReasonStop {
		t.Fatalf("finishReason = %s", out.Result.FinishReason())
	}
}

// Ports TS #23260 collectHarnessAgentToolApprovalContinuations: a synthesized
// execution-denied result for a newly denied approval must not hide that
// approval's response.
func TestCollectToolApprovalContinuations_DeniedApprovalStillContinues(t *testing.T) {
	denied := &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "no"}
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather"},
			types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"},
			types.ToolCallContent{ToolCallID: "call-2", ToolName: "weather"},
			types.ToolApprovalRequestContent{ApprovalID: "approval-2", ToolCallID: "call-2"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
			types.ToolApprovalResponseContent{ApprovalID: "approval-2", Approved: false, Reason: "User denied."},
			types.ToolResultContent{ToolCallID: "call-2", ToolName: "weather", Output: denied},
		}},
	}
	got, err := CollectToolApprovalContinuations(messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ApprovalID != "approval-1" || got[1].ApprovalID != "approval-2" || got[1].Approved {
		t.Fatalf("continuations = %+v, want both decisions", got)
	}

	// A completed (non-denied) result still hides its approval.
	messages[1].Content[2] = types.ToolResultContent{ToolCallID: "call-2", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "done"}}
	got, _ = CollectToolApprovalContinuations(messages)
	if len(got) != 1 || got[0].ApprovalID != "approval-1" {
		t.Fatalf("continuations = %+v, want only approval-1", got)
	}
}
