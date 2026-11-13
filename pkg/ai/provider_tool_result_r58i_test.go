package ai

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r58 integrator: each of the four model tool-result shapes (value and
// pointer, result and error) is marked provider-executed on its own, so each
// stays in the assistant message (TS convertLanguageModelContent).
func TestR58IModelToolResultShapesAreProviderExecuted(t *testing.T) {
	t.Parallel()
	shapes := map[string]types.ContentPart{
		"value result":   types.ToolResultContent{ToolCallID: "c", ToolName: "web_search", Result: "r"},
		"pointer result": &types.ToolResultContent{ToolCallID: "c", ToolName: "web_search", Result: "r"},
		"value error":    types.ToolErrorContent{ToolCallID: "c", ToolName: "web_search", Error: "e"},
		"pointer error":  &types.ToolErrorContent{ToolCallID: "c", ToolName: "web_search", Error: "e"},
	}
	for name, part := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Content: []types.ContentPart{
						types.ToolCallContent{ToolCallID: "c", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
						part,
						types.TextContent{Text: "done"},
					},
					ToolCalls:    []types.ToolCall{{ID: "c", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
					FinishReason: types.FinishReasonStop,
				}, nil
			}}
			res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x"})
			if err != nil {
				t.Fatal(err)
			}
			msgs := res.FinalStep.Response.Messages
			if len(msgs) != 1 || msgs[0].Role != types.RoleAssistant {
				t.Fatalf("response messages = %s, want one assistant message", rolesOf(msgs))
			}
		})
	}
}

func rolesOf(msgs []types.Message) string {
	roles := make([]types.MessageRole, 0, len(msgs))
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	return fmt.Sprint(roles)
}
