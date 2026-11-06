package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// An automatically approved call records its tool-approval-request
// (isAutomatic) and approved tool-approval-response parts before the result,
// as GenerateText does (TS generate-text automatic approvals). The legacy
// loop dropped the approval of locally executed calls.
func TestGenerateAgentAutomaticApprovalRecordsApprovalParts(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{Name: "t1", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusApproved,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
	})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var request *types.ToolApprovalRequestContent
	var response *types.ToolApprovalResponseContent
	sawResult := false
	for _, part := range res.Steps[0].Content {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			request = &p
		case types.ToolApprovalResponseContent:
			response = &p
		case types.ToolResultContent:
			sawResult = true
		}
	}
	if request == nil || !request.IsAutomatic || response == nil || !response.Approved || !sawResult {
		t.Fatalf("content = %+v", res.Steps[0].Content)
	}
}
