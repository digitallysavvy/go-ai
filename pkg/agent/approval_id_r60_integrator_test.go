package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r60 integrator: InternalOptions.ApprovalID names every approval of a step
// (pending request result map, approval request and approval response), and
// without it an approval keeps the call's ID (v0.5.2 behavior).
func TestR60AssignAgentApprovalIDs(t *testing.T) {
	newResults := func() []types.ToolResult {
		return []types.ToolResult{
			{ToolCallID: "a", ToolName: "t", ApprovalStatus: types.ToolApprovalStatusUserApproval,
				Result: map[string]interface{}{"type": "tool-approval-request", "approvalId": "a"}},
			{ToolCallID: "b", ToolName: "t", ApprovalStatus: types.ToolApprovalStatusDenied,
				Result: types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}},
			{ToolCallID: "c", ToolName: "t", Result: "plain"},
		}
	}

	results := newResults()
	original := results[0].Result.(map[string]interface{})
	assignAgentApprovalIDs(results, &ai.InternalOptions{ApprovalID: func(id string) string { return "approval-" + id }})
	if got := results[0].Result.(map[string]interface{})["approvalId"]; got != "approval-a" {
		t.Errorf("pending request approvalId = %v, want approval-a", got)
	}
	if original["approvalId"] != "a" {
		t.Errorf("the original result map was modified: %v", original)
	}
	if results[2].ApprovalID != "" {
		t.Errorf("a result without an approval got ID %q", results[2].ApprovalID)
	}
	var requestIDs, responseIDs []string
	for _, part := range agentToolResultsToContentParts(results, nil) {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			requestIDs = append(requestIDs, p.ApprovalID)
		case types.ToolApprovalResponseContent:
			responseIDs = append(responseIDs, p.ApprovalID)
		}
	}
	if len(requestIDs) != 2 || requestIDs[0] != "approval-a" || requestIDs[1] != "approval-b" {
		t.Errorf("request IDs = %v, want [approval-a approval-b]", requestIDs)
	}
	if len(responseIDs) != 1 || responseIDs[0] != "approval-b" {
		t.Errorf("response IDs = %v, want [approval-b]", responseIDs)
	}

	results = newResults()
	assignAgentApprovalIDs(results, nil)
	for _, part := range agentToolResultsToContentParts(results, nil) {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			if p.ApprovalID != p.ToolCallID {
				t.Errorf("without the option the request ID = %q, want the call ID %q", p.ApprovalID, p.ToolCallID)
			}
		case types.ToolApprovalResponseContent:
			if p.ApprovalID != p.ToolCallID {
				t.Errorf("without the option the response ID = %q, want the call ID %q", p.ApprovalID, p.ToolCallID)
			}
		}
	}
}

// r60 integrator: the Go-only ToolApprover keeps naming its policy in the
// denial reason (v0.5.2 behavior), unlike a ToolApproval denial.
func TestR60ToolApproverDenialNamesPolicy(t *testing.T) {
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("denied tool executed")
			return nil, nil
		}}
	model := &mockLanguageModel{responses: []types.GenerateResult{
		{ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls},
		{Text: "ok", FinishReason: types.FinishReasonStop},
	}}
	res, err := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, MaxSteps: 3,
		ToolApprovalRequired: true, ToolApprover: func(types.ToolCall) bool { return false }}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "Tool call to t was denied by ToolApproval policy."
	found := false
	for _, part := range res.Steps[0].Content {
		if r, ok := part.(types.ToolApprovalResponseContent); ok {
			found = true
			if r.Approved || r.Reason != want {
				t.Fatalf("approval response = %+v, want a denial with reason %q", r, want)
			}
		}
	}
	if !found {
		t.Fatalf("no approval response in %+v", res.Steps[0].Content)
	}
}
