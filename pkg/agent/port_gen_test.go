package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 63593a7f6a: the approval request of an automatically denied tool call is
// not signed, so a client cannot replay it as approved; requests that wait for
// the user or that the policy approved stay signed.
func TestPortgenToolLoopAgentDoesNotSignAutomaticDenials(t *testing.T) {
	secret := []byte("agent-secret")
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied} {
		t.Run(string(status), func(t *testing.T) {
			executed := 0
			tool := approvalSecretTestTool(&executed)
			tool.ToolApproval = ""
			status := status
			agent := NewToolLoopAgent(AgentConfig{
				Model: approvalRequestingModel(),
				Tools: []types.Tool{tool},
				ToolApproval: map[string]interface{}{"tool1": types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
					return types.ToolApprovalResult{Status: status}
				})},
				ExperimentalToolApprovalSecret: secret,
			})
			initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
			res, err := agent.Generate(context.Background(), AgentGenerateOptions{Messages: initial})
			if err != nil {
				t.Fatal(err)
			}
			req := approvalRequest(t, res.ResponseMessages)
			if status == types.ToolApprovalStatusDenied {
				if req.Signature != "" || !req.IsAutomatic {
					t.Fatalf("request = %+v, want an automatic unsigned request", req)
				}
				return
			}
			ok, err := ai.VerifyToolApprovalSignature(secret, req.Signature, req.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "x"})
			if err != nil || !ok {
				t.Fatalf("issued signature does not verify: %v %v", ok, err)
			}
		})
	}
}

// Same as above for the legacy GenerateAgent loop, which builds its own
// approval content parts.
func TestPortgenGenerateAgentDoesNotSignAutomaticDenials(t *testing.T) {
	secret := []byte("agent-secret")
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied} {
		t.Run(string(status), func(t *testing.T) {
			executed := 0
			tool := approvalSecretTestTool(&executed)
			tool.ToolApproval = ""
			status := status
			agent := NewToolLoopAgent(AgentConfig{
				Model: approvalRequestingModel(),
				Tools: []types.Tool{tool},
				ToolApproval: map[string]interface{}{"tool1": types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
					return types.ToolApprovalResult{Status: status}
				})},
				ExperimentalToolApprovalSecret: secret,
			})
			res, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
			if err != nil {
				t.Fatal(err)
			}
			var found *types.ToolApprovalRequestContent
			for _, step := range res.Steps {
				for _, part := range step.Content {
					switch p := part.(type) {
					case types.ToolApprovalRequestContent:
						found = &p
					case *types.ToolApprovalRequestContent:
						found = p
					}
				}
			}
			if found == nil {
				t.Fatalf("no approval request in steps: %+v", res.Steps)
			}
			if status == types.ToolApprovalStatusDenied {
				if found.Signature != "" || !found.IsAutomatic {
					t.Fatalf("request = %+v, want an automatic unsigned request", *found)
				}
				return
			}
			ok, err := ai.VerifyToolApprovalSignature(secret, found.Signature, found.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "x"})
			if err != nil || !ok {
				t.Fatalf("issued signature does not verify: %v %v", ok, err)
			}
		})
	}
}
