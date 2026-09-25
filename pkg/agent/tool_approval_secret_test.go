package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func approvalSecretTestTool(executed *int) types.Tool {
	return types.Tool{
		Name:         "tool1",
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			*executed++
			return "ok", nil
		},
	}
}

func approvalRequestingModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			last := opts.Prompt.Messages[len(opts.Prompt.Messages)-1]
			if last.Role == types.RoleTool {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "x"}}},
			}, nil
		},
	}
}

func approvalRequest(t *testing.T, messages []types.Message) types.ToolApprovalRequestContent {
	t.Helper()
	for _, msg := range messages {
		for _, part := range msg.Content {
			if req, ok := part.(types.ToolApprovalRequestContent); ok {
				return req
			}
		}
	}
	t.Fatalf("no approval request in %+v", messages)
	return types.ToolApprovalRequestContent{}
}

// TS a6463ca: ToolLoopAgent settings accept experimental_toolApprovalSecret;
// issued approvals are signed and forged resumes are rejected.
func TestToolLoopAgent_ToolApprovalSecretFromSettings(t *testing.T) {
	secret := []byte("agent-secret")
	executed := 0
	agent := NewToolLoopAgent(AgentConfig{
		Model:                          approvalRequestingModel(),
		Tools:                          []types.Tool{approvalSecretTestTool(&executed)},
		ExperimentalToolApprovalSecret: secret,
	})
	initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
	res, err := agent.Generate(context.Background(), AgentGenerateOptions{Messages: initial})
	if err != nil {
		t.Fatal(err)
	}
	req := approvalRequest(t, res.ResponseMessages)
	ok, err := ai.VerifyToolApprovalSignature(secret, req.Signature, req.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "x"})
	if err != nil || !ok {
		t.Fatalf("issued signature does not verify: %v %v", ok, err)
	}

	respond := func(signature string) []types.Message {
		history := append([]types.Message(nil), initial...)
		for _, msg := range res.ResponseMessages {
			if msg.Role == types.RoleAssistant {
				content := make([]types.ContentPart, 0, len(msg.Content))
				for _, part := range msg.Content {
					if r, ok := part.(types.ToolApprovalRequestContent); ok {
						r.Signature = signature
						part = r
					}
					content = append(content, part)
				}
				msg.Content = content
			}
			history = append(history, msg)
		}
		return append(history, types.Message{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: req.ApprovalID, Approved: true},
		}})
	}

	if _, err := agent.Generate(context.Background(), AgentGenerateOptions{Messages: respond("forged")}); !ai.IsInvalidToolApprovalSignatureError(err) {
		t.Fatalf("forged resume err = %v", err)
	}
	if executed != 0 {
		t.Fatalf("forged approval executed %d times", executed)
	}
	if _, err := agent.Generate(context.Background(), AgentGenerateOptions{Messages: respond(req.Signature)}); err != nil {
		t.Fatal(err)
	}
	if executed != 1 {
		t.Fatalf("valid approval executed %d times", executed)
	}
}

func TestToolLoopAgent_ToolApprovalSecretFromPrepareCall(t *testing.T) {
	secret := []byte("prepare-call-secret")
	executed := 0
	agent := NewToolLoopAgent(AgentConfig{
		Model: approvalRequestingModel(),
		Tools: []types.Tool{approvalSecretTestTool(&executed)},
		PrepareCall: func(ctx context.Context, cfg PrepareCallConfig) PrepareCallConfig {
			cfg.ExperimentalToolApprovalSecret = secret
			return cfg
		},
	})
	res, err := agent.Generate(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	req := approvalRequest(t, res.ResponseMessages)
	ok, err := ai.VerifyToolApprovalSignature(secret, req.Signature, req.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "x"})
	if err != nil || !ok {
		t.Fatalf("prepareCall secret not applied: %v %v (sig %q)", ok, err, req.Signature)
	}
}
