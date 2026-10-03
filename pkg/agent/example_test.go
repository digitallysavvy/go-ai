package agent_test

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A ToolLoopAgent calls the model, runs the tools it asks for, and repeats
// until the model answers without a tool call or MaxSteps is reached.
func ExampleToolLoopAgent() {
	ctx := context.Background()

	lookup := types.Tool{
		Name:        "population",
		Description: "Look up the population of a city",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{"type": "string"},
			},
			"required": []string{"city"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "about 14 million", nil
		},
	}

	// A mock model stands in for a provider: step 1 calls the tool, step 2 answers.
	step := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step++
			if step == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{{
						ID:        "call_1",
						ToolName:  "population",
						Arguments: map[string]interface{}{"city": "Tokyo"},
					}},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "Tokyo has about 14 million people.",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	myAgent := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:    model,
		System:   "You are a helpful research assistant.",
		Tools:    []types.Tool{lookup},
		MaxSteps: 5,
	})

	result, err := myAgent.Execute(ctx, "What is the population of Tokyo?")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.Text)
	fmt.Println("steps:", len(result.Steps))
	// Output:
	// Tokyo has about 14 million people.
	// steps: 2
}
