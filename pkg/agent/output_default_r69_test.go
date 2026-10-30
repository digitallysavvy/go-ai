package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR69LegacyLoopOutputDefaultFollowsGenerateText: with no Output, the
// legacy loop's AgentResult.Output is the text under TS generateText's rule
// (`output ?? text()`, parsed only for a stop finish or a non-tool-calls
// finish with text): a run that ends on a tool-calls finish has no output.
func TestR69LegacyLoopOutputDefaultFollowsGenerateText(t *testing.T) {
	toolCalls := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Text:         "thinking",
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "x", Arguments: map[string]interface{}{}}},
		}, nil
	}}
	result, err := NewToolLoopAgent(AgentConfig{Model: toolCalls, MaxSteps: 1, Tools: []types.Tool{r67Tool("x")}}).
		GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatalf("GenerateAgent: %v", err)
	}
	if result.Output != nil {
		t.Errorf("tool-calls finish: Output = %#v, want nil", result.Output)
	}

	stop := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
	result, err = NewToolLoopAgent(AgentConfig{Model: stop}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatalf("GenerateAgent: %v", err)
	}
	if result.Output != "done" {
		t.Errorf("stop finish: Output = %#v, want %q", result.Output, "done")
	}
}
