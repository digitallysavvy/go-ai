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

// r62 integrator: TS `result.response?.id ?? generateId()` calls the
// generator only when the response has no ID, so a provider response ID does
// not shift the approval IDs.
func TestR62IntegratorProviderResponseIDDoesNotConsumeGenerator(t *testing.T) {
	newModel := func() *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason:     types.FinishReasonToolCalls,
				ResponseMetadata: &types.ResponseMetadata{ID: "resp-1"},
				ToolCalls:        []types.ToolCall{{ID: "c1", ToolName: "ask", Arguments: map[string]interface{}{}}},
			}, nil
		}}
	}
	executed := 0
	res, err := NewToolLoopAgent(AgentConfig{Model: newModel(), Tools: r62Tools(&executed)}).GenerateAgent(context.Background(), AgentGenerateOptions{
		Prompt: "go", Internal: &ai.InternalOptions{GenerateID: r62Counter()},
	})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: newModel(), Prompt: "go", Tools: r62Tools(&executed), Internal: &ai.InternalOptions{GenerateID: r62Counter()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Steps[0].Response.ID; got != "resp-1" {
		t.Errorf("response ID = %q, want resp-1", got)
	}
	requests, _ := r62ApprovalIDs(res.Steps[0].Content)
	wantRequests, _ := r62ApprovalIDs(gen.Steps[0].Content)
	if fmt.Sprint(requests) != fmt.Sprint(wantRequests) || fmt.Sprint(requests) != "[id-1]" {
		t.Errorf("request IDs = %v, GenerateText %v; want [id-1]", requests, wantRequests)
	}
}

// r62 integrator: a v0.5.2-style history (approval ID == tool call ID) is
// verified against the stored request, so it cannot be spoofed: an unsigned
// request, a signature made for another approval ID, and a response naming
// the call ID while the stored request has a generated ID are all rejected.
func TestR62IntegratorV052ApprovalIDsCannotBeSpoofed(t *testing.T) {
	secret := []byte("r62-integrator-secret")
	executed := 0
	agent := NewToolLoopAgent(AgentConfig{Model: approvalRequestingModel(), Tools: []types.Tool{approvalSecretTestTool(&executed)}, ExperimentalToolApprovalSecret: secret})
	initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
	first, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Messages: initial})
	if err != nil {
		t.Fatal(err)
	}
	req := approvalRequest(t, first.Steps[0].ResponseMessages)
	history := func(requestID, signature, responseID string) []types.Message {
		out := append([]types.Message(nil), initial...)
		for _, msg := range first.Steps[0].ResponseMessages {
			content := make([]types.ContentPart, 0, len(msg.Content))
			for _, part := range msg.Content {
				if r, ok := part.(types.ToolApprovalRequestContent); ok {
					r.ApprovalID = requestID
					r.Signature = signature
					part = r
				}
				content = append(content, part)
			}
			msg.Content = content
			out = append(out, msg)
		}
		return append(out, types.Message{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: responseID, Approved: true},
		}})
	}
	callID := req.ToolCallID
	cases := []struct {
		name    string
		msgs    []types.Message
		wantErr func(error) bool
	}{
		{"unsigned call-ID request", history(callID, "", callID), ai.IsInvalidToolApprovalSignatureError},
		{"generated-ID signature on call-ID request", history(callID, req.Signature, callID), ai.IsInvalidToolApprovalSignatureError},
		{"call-ID response for generated-ID request", history(req.ApprovalID, req.Signature, callID), ai.IsInvalidToolApprovalError},
	}
	for _, tc := range cases {
		_, err := agent.GenerateAgent(context.Background(), AgentGenerateOptions{Messages: tc.msgs})
		if err == nil || !tc.wantErr(err) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	if executed != 0 {
		t.Fatalf("spoofed approvals executed %d times", executed)
	}
}
