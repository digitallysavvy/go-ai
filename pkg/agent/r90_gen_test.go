package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r90genTwoStepModel answers the first call with calls and every later call
// with the text "done".
func r90genTwoStepModel(calls []types.ToolCall) *functionalAgentLanguageModel {
	n := 0
	return &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		n++
		if n == 1 {
			return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
	}}
}

// TS generateText (ToolLoopAgent.generate, WorkflowAgent generate mode): a
// `{type: 'user-approval', reason}` status puts the reason on the
// tool-approval-request, in the step content and in the assistant response
// message. Expected values from a TS 9f7a842ae1 run (generateText with
// toolApproval {a: {type: 'user-approval', reason: 'check'}}).
func TestR90genLegacyUserApprovalReason(t *testing.T) {
	reason := "check"
	a := NewToolLoopAgent(AgentConfig{
		Model: r90genTwoStepModel([]types.ToolCall{{ID: "c1", ToolName: "a", RawArguments: `{}`}, {ID: "c2", ToolName: "b", RawArguments: `{}`}}),
		Tools: []types.Tool{
			{Name: "a", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "r", nil }},
			{Name: "b", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, errors.New("boom")
			}},
		},
		ToolApproval: map[string]types.ToolApprovalValue{"a": types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval, Reason: &reason}},
		StopWhen:     []ai.StopCondition{ai.IsStepCount(3)},
	})
	result, err := a.Execute(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("steps = %d, want 1 (the loop waits for the approval)", len(result.Steps))
	}
	step := result.Steps[0]
	var contentReason, messageReason string
	found := 0
	for _, part := range step.Content {
		if p, ok := part.(types.ToolApprovalRequestContent); ok {
			found++
			contentReason = p.Reason
		}
	}
	for _, m := range step.Response.Messages {
		for _, part := range m.Content {
			if p, ok := part.(types.ToolApprovalRequestContent); ok {
				found++
				messageReason = p.Reason
			}
		}
	}
	if found != 2 || contentReason != "check" || messageReason != "check" {
		t.Fatalf("approval request reasons: found %d, content %q, message %q; want 2, \"check\", \"check\"", found, contentReason, messageReason)
	}
}
