package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r63ContentShape(content []types.ContentPart) string {
	var out []string
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolCallContent:
			out = append(out, "call:"+p.ToolCallID)
		case types.ToolApprovalRequestContent:
			out = append(out, fmt.Sprintf("request:%s:%s:auto=%v", p.ToolCallID, p.ApprovalID, p.IsAutomatic))
		case types.ToolApprovalResponseContent:
			out = append(out, fmt.Sprintf("response:%s:%s:approved=%v", p.ToolCallID, p.ApprovalID, p.Approved))
		case types.ToolResultContent:
			out = append(out, "result:"+p.ToolCallID)
		case types.ToolErrorContent:
			out = append(out, "error:"+p.ToolCallID)
		default:
			out = append(out, fmt.Sprintf("%T", part))
		}
	}
	return strings.Join(out, " ")
}

// r63: an automatically approved local call that does not run (its tool has
// no Execute, or a length finish keeps every tool from running) still gets
// an approval ID from the run's generator, in call order, and its approval
// request and response parts, as in TS generateText (and GenerateText); it
// is not a tool result and the run stops.
func TestR63GenerateAgentApprovedCallThatDoesNotRunKeepsItsApprovalParts(t *testing.T) {
	for _, reason := range []types.FinishReason{types.FinishReasonToolCalls, types.FinishReasonLength} {
		calls := []types.ToolCall{
			{ID: "c1", ToolName: "client", Arguments: map[string]interface{}{}},
			{ID: "c2", ToolName: "local", Arguments: map[string]interface{}{}},
		}
		modelCalls := 0
		newModel := func() *testutil.MockLanguageModel {
			return &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				modelCalls++
				return &types.GenerateResult{ToolCalls: calls, FinishReason: reason}, nil
			}}
		}
		tools := []types.Tool{
			{Name: "client", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusApproved},
			{Name: "local", Parameters: map[string]interface{}{"type": "object"}, ToolApproval: types.ToolApprovalStatusApproved,
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "ok", nil
				}},
		}
		res, err := NewToolLoopAgent(AgentConfig{Model: newModel(), Tools: tools, StopWhen: []ai.StopCondition{ai.IsStepCount(3)}}).GenerateAgent(context.Background(), AgentGenerateOptions{
			Prompt: "go", Internal: &ai.InternalOptions{GenerateID: r62Counter()},
		})
		if err != nil {
			t.Fatal(err)
		}
		agentCalls := modelCalls
		gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
			Model: newModel(), Prompt: "go", Tools: tools, StopWhen: []ai.StopCondition{ai.IsStepCount(3)},
			Internal: &ai.InternalOptions{GenerateID: r62Counter()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if agentCalls != 1 || len(res.Steps) != 1 {
			t.Fatalf("%s: agent made %d model calls, %d steps; want 1", reason, agentCalls, len(res.Steps))
		}
		want := "call:c1 call:c2 request:c1:id-2:auto=true request:c2:id-3:auto=true response:c1:id-2:approved=true response:c2:id-3:approved=true"
		if reason == types.FinishReasonToolCalls {
			want += " result:c2"
		}
		if got := r63ContentShape(res.Steps[0].Content); got != want {
			t.Errorf("%s: agent content\n got %s\nwant %s", reason, got, want)
		}
		if got, gotGen := r63ContentShape(res.Steps[0].Content), r63ContentShape(gen.Steps[0].Content); got != gotGen {
			t.Errorf("%s: agent content differs from GenerateText\n agent %s\n  gen  %s", reason, got, gotGen)
		}
		for _, r := range res.Steps[0].ToolResults {
			if r.ToolCallID == "c1" {
				t.Errorf("%s: step tool results include the call that did not run: %+v", reason, r)
			}
		}
		var tool []string
		for _, m := range res.Steps[0].ResponseMessages {
			if m.Role == types.RoleTool {
				tool = append(tool, r63ContentShape(m.Content))
			}
		}
		wantTool := "response::id-2:approved=true response::id-3:approved=true"
		if reason == types.FinishReasonToolCalls {
			wantTool += " result:c2"
		}
		if got := strings.Join(tool, " | "); got != wantTool {
			t.Errorf("%s: agent tool message\n got %s\nwant %s", reason, got, wantTool)
		}
	}
}
