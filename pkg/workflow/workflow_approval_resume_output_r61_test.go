package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS workflow-agent-approval.test.ts "issues signed data and resumes without
// a writable": the request is approval-call-1, signed; the resumed generate
// run's first response message is the tool message whose result output is
// the model output ({type: text, value: executed}, or execution-denied), as
// TS modelResult (no input). Go kept the raw result and the input there.
func TestR61WorkflowResumedApprovalResultIsTheModelOutput(t *testing.T) {
	prompt := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Perform the action."}}}}
	secret := []byte("approval-test-secret")
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}
	for _, approved := range []bool{true, false} {
		t.Run(fmt.Sprint("approved=", approved), func(t *testing.T) {
			executed := 0
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: &wfScriptModel{responses: []types.GenerateResult{
					{Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "call-1", ToolName: "action", Input: `{"value":"requested"}`}}, ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "action", RawArguments: `{"value":"requested"}`}}, FinishReason: types.FinishReasonToolCalls},
					{Content: []types.ContentPart{types.TextContent{Text: "Done"}}, FinishReason: types.FinishReasonStop},
				}},
				Tools: []types.Tool{{Name: "action", Parameters: schema, ToolApproval: true, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					executed++
					return "executed", nil
				}}},
				ExperimentalToolApprovalSecret: secret,
			})
			if err != nil {
				t.Fatal(err)
			}
			issued, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: prompt})
			if err != nil {
				t.Fatal(err)
			}
			var request types.ToolApprovalRequestContent
			for _, step := range issued.Steps {
				for _, p := range step.Content {
					if r, ok := p.(types.ToolApprovalRequestContent); ok {
						request = r
					}
				}
			}
			if request.ApprovalID != "approval-call-1" || request.ToolCall.ID != "call-1" {
				t.Fatalf("request = %+v", request)
			}
			if ok, err := ai.VerifyToolApprovalSignature(secret, request.Signature, request.ApprovalID, "call-1", "action", map[string]interface{}{"value": "requested"}); err != nil || !ok {
				t.Fatalf("signature: ok=%v err=%v", ok, err)
			}
			if executed != 0 {
				t.Fatal("executed before approval")
			}
			resume := append(append(append([]types.Message{}, prompt...), issued.ResponseMessages...), types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: approved}}})
			resumed, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: resume})
			if err != nil {
				t.Fatal(err)
			}
			if resumed.Text != "Done" {
				t.Fatalf("text = %q", resumed.Text)
			}
			want := 0
			if approved {
				want = 1
			}
			if executed != want {
				t.Fatalf("executed %d times, want %d", executed, want)
			}
			first := resumed.ResponseMessages[0]
			if first.Role != types.RoleTool || len(first.Content) != 1 {
				t.Fatalf("first response message = %+v", first)
			}
			part, ok := first.Content[0].(types.ToolResultContent)
			if !ok || part.Output == nil || part.Input != nil || part.Result != nil {
				t.Fatalf("tool result = %+v", first.Content[0])
			}
			if approved && (part.Output.Type != types.ToolResultOutputText || part.Output.Value != "executed") {
				t.Fatalf("output = %+v, want text executed", part.Output)
			}
			if !approved && part.Output.Type != types.ToolResultOutputExecutionDenied {
				t.Fatalf("output = %+v, want execution-denied", part.Output)
			}
		})
	}
}
