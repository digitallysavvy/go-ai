package workflow

import (
	"context"
	"errors"
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
