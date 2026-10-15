package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS ToolLoopAgent runs generateText, so a required tool choice that produces
// no tool call fails with ToolChoiceViolationError carrying the response content.
func TestLegacyLoopEnforcesRequiredToolChoice(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		Text:         "No tool call.",
		Content:      []types.ContentPart{types.ReasoningContent{Text: "I will not call the tool."}},
		FinishReason: types.FinishReasonStop,
	}}}
	tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired}})
	_, err := a.Execute(context.Background(), "test-input")
	var v *ai.ToolChoiceViolationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want ToolChoiceViolationError", err)
	}
	if v.FinishReason != types.FinishReasonStop || len(v.Content) != 2 {
		t.Fatalf("violation = %#v", v)
	}
	if _, ok := v.Content[0].(types.ReasoningContent); !ok {
		t.Fatalf("content[0] = %T", v.Content[0])
	}
	if text, ok := v.Content[1].(types.TextContent); !ok || text.Text != "No tool call." {
		t.Fatalf("content[1] = %#v", v.Content[1])
	}
}
