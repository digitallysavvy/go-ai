package workflow

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r60 integrator: an approval persisted by v0.5.2 WorkflowAgent.Generate (its
// ID is the tool call ID, signed with that ID) still resumes after the switch
// to `approval-<toolCallId>`: the response is matched and verified against the
// stored request's ID.
func TestR60WorkflowResumesV052ApprovalID(t *testing.T) {
	secret := []byte("s")
	var executed atomic.Int32
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{calls: 1},
		Tools: []types.Tool{{Name: "t1", ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executed.Add(1)
				return "done", nil
			}}},
		ExperimentalToolApprovalSecret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]interface{}{}
	signature, err := ai.SignToolApproval(secret, "1", "1", "t1", args)
	if err != nil {
		t.Fatal(err)
	}
	call := types.ToolCall{ID: "1", ToolName: "t1", Arguments: args}
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "1", ToolName: "t1", Input: `{}`, Arguments: args},
			types.ToolApprovalRequestContent{ApprovalID: "1", ToolCallID: "1", ToolCall: call, Signature: signature},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "1", Approved: true},
		}},
	}
	if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages}); err != nil {
		t.Fatal(err)
	}
	if executed.Load() != 1 {
		t.Fatalf("tool executed %d times after the v0.5.2 approval, want 1", executed.Load())
	}
}
