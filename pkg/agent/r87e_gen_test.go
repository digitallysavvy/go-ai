package agent

// Round 87e gen: known differences that are now fixed (TS 3fab50aa9b).

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS ToolLoopAgent runs generateText, whose executed tool-result has no title
// (execute-tool-call.ts). Known difference "Executed tool results carry the
// tool title".
func TestR87egenAgentExecutedToolResultHasNoTitle(t *testing.T) {
	n := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		n++
		if n > 1 {
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls,
			Content:   []types.ContentPart{types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Input: `{}`}},
			ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "tool1", RawArguments: `{}`}}}, nil
	}}
	tool := types.Tool{Name: "tool1", Title: "Tool One", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	res, err := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatalf("GenerateAgent() error = %v", err)
	}
	if len(res.Steps) == 0 || len(res.Steps[0].ToolResults) != 1 {
		t.Fatalf("steps = %+v, want one tool result in step 0", res.Steps)
	}
	if got := res.Steps[0].ToolResults[0].Title; got != "" {
		t.Errorf("tool result title = %q, want none", got)
	}
	if got := res.Steps[0].ToolCalls[0].Title; got != "Tool One" {
		t.Errorf("tool call title = %q, want Tool One", got)
	}
}
