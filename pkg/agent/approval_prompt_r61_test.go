package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The approval parts of an automatically approved (or denied) call stay out
// of the next model call's prompt, as TS convertToLanguageModelPrompt and
// GenerateText do. The legacy loop sent them, so the OpenAI Responses
// converter turned a local approval into an mcp_approval_response item.
func TestR61GenerateAgentPromptOmitsLocalApprovalParts(t *testing.T) {
	execOK := func(ctx context.Context, args map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}
	newResp := func() []types.GenerateResult {
		return []types.GenerateResult{{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: "{}"},
				types.ToolCallContent{ToolCallID: "c2", ToolName: "u", Input: "{}"},
			},
			ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", RawArguments: "{}"}, {ID: "c2", ToolName: "u", RawArguments: "{}"}},
			FinishReason: types.FinishReasonToolCalls,
		}}
	}
	tools := []types.Tool{{Name: "t", Execute: execOK}, {Name: "u", Execute: execOK}}
	approval := map[string]interface{}{"t": types.ToolApprovalStatusApproved, "u": types.ToolApprovalStatusDenied}

	am := &mockLanguageModel{responses: newResp()}
	a := NewToolLoopAgent(AgentConfig{Model: am, Tools: tools, ToolApproval: approval})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	gm := &mockLanguageModel{responses: newResp()}
	if _, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: gm, Prompt: "go", Tools: tools, ToolApproval: approval, StopWhen: []ai.StopCondition{ai.IsStepCount(20)}}); err != nil {
		t.Fatal(err)
	}
	if len(am.options) != 2 || len(gm.options) != 2 {
		t.Fatalf("model calls: agent %d, generate %d", len(am.options), len(gm.options))
	}
	for _, msg := range am.options[1].Prompt.Messages {
		for _, part := range msg.Content {
			switch part.(type) {
			case types.ToolApprovalRequestContent, types.ToolApprovalResponseContent:
				t.Fatalf("approval part %T reached the model prompt: %+v", part, am.options[1].Prompt.Messages)
			}
		}
	}
	got, _ := json.Marshal(am.options[1].Prompt.Messages)
	want, _ := json.Marshal(gm.options[1].Prompt.Messages)
	if string(got) != string(want) {
		t.Fatalf("second prompt:\nagent    %s\ngenerate %s", got, want)
	}
}
