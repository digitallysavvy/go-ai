package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
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

// TS 0b8cbfadb8 for the legacy GenerateAgent loop: PrepareCall returns the same
// messages slice for every step, yet each step's approval function gets a
// slice of its own, shared by the calls of that step.
func TestPortgenGenerateAgentApprovalMessagesSlicePerStep(t *testing.T) {
	prepared := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prepared-input"}}}}
	var collected [][]types.Message
	responses := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step := responses
			responses++
			if step < 2 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{
					{ID: fmt.Sprintf("call-%d-1", step), ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}},
					{ID: fmt.Sprintf("call-%d-2", step), ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}},
				}}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	executed := 0
	tool := approvalSecretTestTool(&executed)
	tool.ToolApproval = ""
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{tool},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			collected = append(collected, o.Messages)
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}),
		PrepareCall: func(_ context.Context, cfg PrepareCallConfig) PrepareCallConfig {
			cfg.Messages = prepared
			return cfg
		},
		MaxSteps: 3,
	})
	if _, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "test-input"}); err != nil {
		t.Fatal(err)
	}
	if len(collected) != 4 {
		t.Fatalf("approval calls = %d, want 4", len(collected))
	}
	same := func(a, b []types.Message) bool { return len(a) > 0 && len(b) > 0 && &a[0] == &b[0] }
	for i, msgs := range collected {
		if same(msgs, prepared) {
			t.Errorf("call %d got the slice PrepareCall returned", i)
		}
	}
	if !same(collected[0], collected[1]) || !same(collected[2], collected[3]) || same(collected[0], collected[2]) {
		t.Fatalf("identity: 0=1 %v, 2=3 %v, 0=2 %v", same(collected[0], collected[1]), same(collected[2], collected[3]), same(collected[0], collected[2]))
	}
}
