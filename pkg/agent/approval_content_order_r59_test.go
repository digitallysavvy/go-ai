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

func r59PartKinds(parts []types.ContentPart) string {
	var kinds []string
	for _, p := range parts {
		switch v := p.(type) {
		case types.ToolCallContent:
			kinds = append(kinds, "call:"+v.ToolCallID)
		case types.ToolResultContent:
			kinds = append(kinds, "result:"+v.ToolCallID)
		case types.ToolApprovalRequestContent:
			kinds = append(kinds, "request:"+v.ToolCallID)
		case types.ToolApprovalResponseContent:
			kinds = append(kinds, "response:"+v.ToolCallID)
		default:
			kinds = append(kinds, fmt.Sprintf("%T", p))
		}
	}
	return strings.Join(kinds, " ")
}

// TS convertLanguageModelContent orders the executed tool outputs as: outputs
// without an approval response, then the approval requests, the approval
// responses, then the outputs with one. The legacy agent loop interleaved each
// call's request, response and output in call order, unlike GenerateText.
func TestR59GenerateAgentApprovalContentOrderMatchesGenerateText(t *testing.T) {
	newModel := func() *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{
					{ID: "a", ToolName: "approved", RawArguments: "{}", Arguments: map[string]interface{}{}},
					{ID: "b", ToolName: "plain", RawArguments: "{}", Arguments: map[string]interface{}{}},
				},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		}}
	}
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}
	tools := []types.Tool{
		{Name: "approved", ToolApproval: types.ToolApprovalStatusApproved, Execute: exec},
		{Name: "plain", Execute: exec},
	}
	one := 1
	gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: newModel(), Prompt: "go", Tools: tools, MaxSteps: &one})
	if err != nil {
		t.Fatal(err)
	}
	want := r59PartKinds(gen.Steps[0].Content)
	a := NewToolLoopAgent(AgentConfig{Model: newModel(), Tools: tools, MaxSteps: 1})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r59PartKinds(res.Steps[0].Content); got != want {
		t.Fatalf("agent step content = %q, want GenerateText's %q", got, want)
	}
}
