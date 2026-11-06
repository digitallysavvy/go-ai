package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertLanguageModelContent marks every tool-result the model returns
// as providerExecuted, so the legacy loop's step response keeps it in the
// assistant message next to its call rather than in a tool message.
func TestR58GenerateAgentModelToolResultsAreProviderExecuted(t *testing.T) {
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
			types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", Result: "r"},
			types.TextContent{Text: "done"},
		},
		ToolCalls:    []types.ToolCall{{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
		FinishReason: types.FinishReasonStop,
	}}}
	a := NewToolLoopAgent(AgentConfig{Model: model})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("no steps")
	}
	step := res.Steps[len(res.Steps)-1]
	for _, part := range step.Content {
		if p, ok := part.(types.ToolResultContent); ok && !p.ProviderExecuted {
			t.Fatalf("step tool result %s ProviderExecuted = false", p.ToolCallID)
		}
	}
	for _, m := range step.Response.Messages {
		if m.Role == types.RoleTool {
			t.Fatalf("provider tool result went to a tool message: %+v", step.Response.Messages)
		}
	}
}
