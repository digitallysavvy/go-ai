package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertLanguageModelContent gives a provider tool-approval-request its
// parsed call, and throws ToolCallNotFoundForApprovalError for one without a
// call. The legacy loop kept the request without its call, so the
// provider-approval pause (which reads ToolCall.ProviderExecuted) never
// fired, and it accepted a request for an unknown call.
func TestR60GenerateAgentProviderApprovalRequestTakesItsToolCall(t *testing.T) {
	mk := func(requestToolCallID string) *mockLanguageModel {
		call := types.ToolCall{ID: "c1", ToolName: "mcp", Arguments: map[string]interface{}{}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}
		return &mockLanguageModel{responses: []types.GenerateResult{
			{
				ToolCalls: []types.ToolCall{call},
				Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "c1", ToolName: "mcp", Input: `{}`, ProviderExecuted: true, Dynamic: true},
					types.ToolApprovalRequestContent{ApprovalID: "ap1", ToolCallID: requestToolCallID},
				},
				FinishReason: types.FinishReasonToolCalls,
			},
			{Text: "should not run", FinishReason: types.FinishReasonStop},
		}}
	}
	a := NewToolLoopAgent(AgentConfig{Model: mk("c1"), MaxSteps: 3})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %d, want 1 (the provider approval request pauses the loop)", len(res.Steps))
	}
	found := false
	for _, part := range res.Steps[0].Content {
		if p, ok := part.(types.ToolApprovalRequestContent); ok {
			found = p.ToolCall.ID == "c1" && p.ToolCall.ProviderExecuted
		}
	}
	if !found {
		t.Fatalf("content = %+v, want the approval request with its call", res.Steps[0].Content)
	}

	_, err = NewToolLoopAgent(AgentConfig{Model: mk("missing"), MaxSteps: 3}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if !ai.IsToolCallNotFoundForApprovalError(err) {
		t.Fatalf("err = %v, want ToolCallNotFoundForApprovalError", err)
	}
}
