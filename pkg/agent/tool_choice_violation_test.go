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

// TS #23843: tool choice none rejects every tool call and never executes it.
func TestLegacyLoopRejectsToolCallsWithToolChoiceNone(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
	}}}
	executed := false
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed = true
			return "ok", nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone}})
	_, err := a.Execute(context.Background(), "test-input")
	var v *ai.ToolChoiceViolationError
	if !errors.As(err, &v) || v.ToolChoice.Type != types.ToolChoiceNone {
		t.Fatalf("err = %v, want ToolChoiceViolationError(none)", err)
	}
	if executed {
		t.Fatal("tool must not execute with tool choice none")
	}
}
