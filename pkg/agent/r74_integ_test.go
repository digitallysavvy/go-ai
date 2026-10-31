package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r74 mutation survivor (gen fix 5): with a ToModelOutput tool the legacy
// loop's step content keeps the raw tool output in Result next to the model
// output (TS content `output` is the raw value whatever toModelOutput returns).
func TestR74Integ_AgentToModelOutputContentKeepsRawResult(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{
		Model: r72agentToolModel(),
		Tools: []types.Tool{{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"n": 1}, nil
			},
			ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees n"}, nil
			},
		}},
	})
	res, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	var tr *types.ToolResultContent
	for _, p := range res.Steps[0].Content {
		if v, ok := p.(types.ToolResultContent); ok {
			tr = &v
		}
	}
	if tr == nil || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "model sees n" {
		t.Fatalf("content = %#v", res.Steps[0].Content)
	}
	if m, ok := tr.Result.(map[string]interface{}); !ok || m["n"] != 1 {
		t.Fatalf("Result = %#v, want the raw tool output", tr.Result)
	}
}
