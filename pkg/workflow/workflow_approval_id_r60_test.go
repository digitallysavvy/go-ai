package workflow

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent names the approval of tool call X `approval-X` (signed
// with that ID) in generate and in stream mode. Go used the tool call ID
// (generate, through the legacy loop) or a random ID (stream, through
// StreamText).
func TestR60WorkflowApprovalIDsMatchTS(t *testing.T) {
	secret := []byte("s")
	var executed atomic.Int32
	newAgent := func(model interface{}) *WorkflowAgent {
		cfg := WorkflowAgent{
			Tools: []types.Tool{{Name: "t1", ToolApproval: true,
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					executed.Add(1)
					return "done", nil
				}}},
			ExperimentalToolApprovalSecret: secret,
		}
		switch m := model.(type) {
		case *wfMockModel:
			cfg.Model = m
		case *wfStreamToolCallModel:
			cfg.Model = m
		}
		a, err := NewWorkflowAgent(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	requestOf := func(t *testing.T, steps []types.StepResult) types.ToolApprovalRequestContent {
		t.Helper()
		for _, step := range steps {
			for _, part := range step.Content {
				if req, ok := part.(types.ToolApprovalRequestContent); ok {
					return req
				}
			}
		}
		t.Fatalf("no approval request in %+v", steps)
		return types.ToolApprovalRequestContent{}
	}
	check := func(t *testing.T, steps []types.StepResult) {
		t.Helper()
		req := requestOf(t, steps)
		if req.ApprovalID != "approval-1" {
			t.Fatalf("approval ID = %q, want approval-1", req.ApprovalID)
		}
		if ok, err := ai.VerifyToolApprovalSignature(secret, req.Signature, "approval-1", "1", "t1", req.ToolCall.Arguments); err != nil || !ok {
			t.Fatalf("signature does not verify for approval-1: ok=%v err=%v", ok, err)
		}
		inMessages := false
		for _, step := range steps {
			for _, msg := range step.Response.Messages {
				for _, part := range msg.Content {
					if r, ok := part.(types.ToolApprovalRequestContent); ok && r.ApprovalID == "approval-1" {
						inMessages = true
					}
				}
			}
		}
		if !inMessages {
			t.Fatal("approval-1 request missing from the response messages")
		}
	}

	t.Run("generate", func(t *testing.T) {
		res, err := newAgent(&wfMockModel{}).GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go"})
		if err != nil {
			t.Fatal(err)
		}
		check(t, res.Steps)
		// The caller answers approval-1; the resumed run executes the tool.
		messages := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
		for _, step := range res.Steps {
			messages = append(messages, step.Response.Messages...)
		}
		messages = append(messages, types.Message{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
		}})
		executed.Store(0)
		if _, err := newAgent(&wfMockModel{calls: 1}).GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages}); err != nil {
			t.Fatal(err)
		}
		if executed.Load() != 1 {
			t.Fatalf("tool executed %d times after approval-1, want 1", executed.Load())
		}
	})
	t.Run("stream", func(t *testing.T) {
		res, err := newAgent(&wfStreamToolCallModel{}).StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go"})
		if err != nil {
			t.Fatal(err)
		}
		for {
			if _, e := res.Stream().Next(); e != nil {
				if e != io.EOF {
					t.Fatal(e)
				}
				break
			}
		}
		check(t, res.Steps())
	})
}
