package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r58 integrator: the legacy loop marks each of the four model tool-result
// shapes (value and pointer, result and error) provider-executed, so none
// goes to a tool message (TS convertLanguageModelContent).
func TestR58IGenerateAgentModelToolResultShapes(t *testing.T) {
	shapes := map[string]types.ContentPart{
		"value result":   types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", Result: "r"},
		"pointer result": &types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", Result: "r"},
		"value error":    types.ToolErrorContent{ToolCallID: "ws", ToolName: "web_search", Error: "e"},
		"pointer error":  &types.ToolErrorContent{ToolCallID: "ws", ToolName: "web_search", Error: "e"},
	}
	for name, part := range shapes {
		t.Run(name, func(t *testing.T) {
			model := &mockLanguageModel{responses: []types.GenerateResult{{
				Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
					part,
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
			for _, m := range res.Steps[len(res.Steps)-1].Response.Messages {
				if m.Role == types.RoleTool {
					t.Fatalf("model tool result went to a tool message: %+v", m)
				}
			}
		})
	}
}
