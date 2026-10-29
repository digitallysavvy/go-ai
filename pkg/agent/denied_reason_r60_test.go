package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS (and GenerateText) leave a denial's reason unset when the approval gives
// none, so the provider converter applies its own default denial text. The
// legacy loop wrote "Tool call to X was denied by ToolApproval policy." into
// the approval response and the execution-denied output the model sees.
func TestR60GenerateAgentToolApprovalDenialKeepsReasonUnset(t *testing.T) {
	deny := map[string]types.ToolApprovalValue{
		"t": types.SingleToolApprovalFunc(func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
		}),
	}
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("denied tool executed")
			return nil, nil
		}}
	model := &mockLanguageModel{responses: []types.GenerateResult{
		{ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls},
		{Text: "ok", FinishReason: types.FinishReasonStop},
	}}
	res, err := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, ToolApproval: deny, MaxSteps: 3}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var responses int
	for _, part := range res.Steps[0].Content {
		if r, ok := part.(types.ToolApprovalResponseContent); ok {
			responses++
			if r.Approved || r.Reason != "" {
				t.Fatalf("approval response = %+v, want a denial without a reason", r)
			}
		}
	}
	if responses != 1 {
		t.Fatalf("approval responses = %d, want 1", responses)
	}
	if dump := fmt.Sprintf("%+v", res.Steps[0].Response.Messages); strings.Contains(dump, "denied by ToolApproval policy") {
		t.Fatalf("response messages carry the synthesized reason: %s", dump)
	}

	// GenerateText gives the same step content and messages.
	gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: &mockLanguageModel{responses: model.responses}, Prompt: "go", Tools: []types.Tool{tool}, ToolApproval: deny, StopWhen: []ai.StopCondition{ai.IsStepCount(3)}})
	if err != nil {
		t.Fatal(err)
	}
	deniedOutput := func(messages []types.Message) *types.ToolResultOutput {
		for _, msg := range messages {
			for _, part := range msg.Content {
				if r, ok := part.(types.ToolResultContent); ok && r.Output != nil && r.Output.Type == types.ToolResultOutputExecutionDenied {
					return r.Output
				}
			}
		}
		return nil
	}
	got, want := deniedOutput(res.Steps[0].Response.Messages), deniedOutput(gen.Steps[0].Response.Messages)
	if got == nil || want == nil || got.Reason != want.Reason || got.Reason != "" {
		t.Fatalf("legacy denied output = %+v, GenerateText = %+v; want both without a reason", got, want)
	}
}
