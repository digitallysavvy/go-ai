package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS automatic denials send `providerExecuted: toolCall.providerExecuted`, so
// the step's approval response keeps a call's explicit false.
func TestToolLoopAgentExecute_AutomaticDenialKeepsExplicitProviderExecutedFalse(t *testing.T) {
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
	agent := NewToolLoopAgent(AgentConfig{
		Model:        model,
		ToolApproval: map[string]types.ToolApprovalValue{"noop": types.ToolApprovalStatusDenied},
		Tools: []types.Tool{{
			Name: "noop",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
	})
	result, err := agent.Execute(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range result.Steps {
		for _, part := range step.Content {
			if resp, ok := part.(types.ToolApprovalResponseContent); ok {
				found = true
				if resp.ProviderExecuted || !resp.ProviderExecutedSet || !resp.ToolCall.ProviderExecutedSet {
					t.Fatalf("approval response = %+v, want explicit providerExecuted false", resp)
				}
			}
		}
	}
	if !found {
		t.Fatal("no approval response in the steps")
	}
}
