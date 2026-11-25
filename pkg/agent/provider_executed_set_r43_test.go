package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The AgentResult loop hands ToModelOutput the tool call with an explicit
// providerExecuted: false kept (types.ToolCall.ProviderExecutedSet).
func TestToolLoopAgentExecute_ToModelOutputKeepsProviderExecutedSet(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}, ProviderExecutedSet: true}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var seen *types.ToolCall
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{
			Name: "noop",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
			ToModelOutput: func(_ context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				mu.Lock()
				defer mu.Unlock()
				seen = o.ToolCall
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}, nil
			},
		}},
	})
	if _, err := agent.Execute(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen == nil || !seen.ProviderExecutedSet {
		t.Fatalf("ToModelOutput tool call = %#v", seen)
	}
}
