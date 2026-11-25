package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ruling (c)(2): TS ToolLoopAgent.generate is generateText, whose tool-error
// for an invalid client call is `{toolCallId, toolName, input, error,
// dynamic: true}` (generate-text.ts 1484-1492): no toolMetadata, no title.
// The invalid tool-call part keeps the metadata.
func TestR78GenAgentInvalidToolErrorWithoutToolMetadata(t *testing.T) {
	tool := types.Tool{
		Name:       "tool1",
		Title:      "Tool One",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Metadata:   map[string]interface{}{"owner": "team"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: r52ToolCallModel(types.ToolCall{ID: "bad", ToolName: "tool1", RawArguments: `{}`}), Tools: []types.Tool{tool}})
	res, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	var sawCall, sawErr bool
	for _, part := range res.Steps[0].Content {
		switch p := part.(type) {
		case types.ToolCallContent:
			sawCall = true
			if !p.Invalid || p.ToolMetadata["owner"] != "team" {
				t.Fatalf("tool call = %#v, want invalid with tool metadata", p)
			}
		case types.ToolErrorContent:
			sawErr = true
			if p.ToolMetadata != nil || p.Title != "" || !p.Dynamic {
				t.Fatalf("tool error = %#v, want dynamic without metadata or title", p)
			}
		}
	}
	if !sawCall || !sawErr {
		t.Fatalf("content = %#v", res.Steps[0].Content)
	}
}
