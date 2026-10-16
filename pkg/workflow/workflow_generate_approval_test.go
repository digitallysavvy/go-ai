package workflow

import (
	"context"
	"errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func approvalTool() types.Tool {
	return types.Tool{Name: "t1", ToolApproval: true,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "x", nil
		}}
}

// A tool call awaiting user approval pauses generate (finish reason
// tool-calls, one model call) instead of re-prompting the model with an
// unresolved tool call.
func TestWorkflowGeneratePausesOnApprovalRequest(t *testing.T) {
	m := &wfMockModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{approvalTool()}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(res.Steps) != 1 || res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("calls=%d steps=%d finish=%v", m.calls, len(res.Steps), res.FinishReason)
	}
}

// TS convertToLanguageModelPrompt: a history with an unresolved tool call
// fails with MissingToolResultsError.
func TestWorkflowGenerateRejectsMissingToolResults(t *testing.T) {
	m := &wfMockModel{}
	a, _ := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{approvalTool()}})
	_, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "t1", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1"},
		}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
	}})
	var me *types.MissingToolResultsError
	if !errors.As(err, &me) || m.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, m.calls)
	}
}

// TS generate: result.response.messages starts with the tool message holding
// the results of the approvals the run resumed, so callers appending the
// response to their history do not re-execute those approvals next turn.
func TestWorkflowGenerateResponseMessagesIncludeResumedApprovalResults(t *testing.T) {
	m := &wfJSONModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{
		Name:         "getWeather",
		ToolApproval: true,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather?"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "getWeather", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: true},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ResponseMessages) < 2 {
		t.Fatalf("response messages = %+v, want the resumed tool message then the assistant reply", res.ResponseMessages)
	}
	first := res.ResponseMessages[0]
	found := false
	for _, part := range first.Content {
		if r, ok := part.(types.ToolResultContent); ok && r.ToolCallID == "call-1" {
			found = true
		}
	}
	if first.Role != types.RoleTool || !found {
		t.Fatalf("first response message = %+v, want tool result for call-1", first)
	}
	if last := res.ResponseMessages[len(res.ResponseMessages)-1]; last.Role != types.RoleAssistant {
		t.Fatalf("last response message role = %s, want assistant", last.Role)
	}
}

// TS onAbort receives the steps completed before the abort.
func TestWorkflowGenerateOnAbortReceivesCompletedSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &ctxAwareModel{} // step 1: tool call t1; later steps fail once ctx is done
	var got []types.StepResult
	called := 0
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{
		Name: "t1",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel() // abort after the first step's tool ran
			return "ok", nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.GenerateWithOptions(ctx, WorkflowGenerateOptions{
		Prompt: "hi",
		OnAbort: func(_ context.Context, steps []types.StepResult) {
			called++
			got = steps
		},
	})
	if err == nil {
		t.Fatal("expected an abort error")
	}
	if called != 1 || len(got) != 1 || len(got[0].ToolCalls) != 1 {
		t.Fatalf("OnAbort called=%d steps=%+v, want the one completed step", called, got)
	}
}

type ctxAwareModel struct{ wfMockModel }

func (m *ctxAwareModel) DoGenerate(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.wfMockModel.DoGenerate(ctx, o)
}

// TS stream() awaits convertToLanguageModelPrompt, so a history with an
// unresolved tool call rejects stream() itself with MissingToolResultsError.
func TestWorkflowStreamRejectsMissingToolResults(t *testing.T) {
	m := &wfMockModel{}
	a, _ := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{approvalTool()}})
	_, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "t1", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1"},
		}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
	}})
	var me *types.MissingToolResultsError
	if !errors.As(err, &me) || m.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, m.calls)
	}
}
