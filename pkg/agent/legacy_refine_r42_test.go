package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS parseToolCall runs refineToolInput, so the language-model-call end
// event and the tool's onInputAvailable already see the refined input. The
// AgentResult loop refined the calls only after both.
func TestToolLoopAgentExecute_RefineToolInputRunsInParseStep(t *testing.T) {
	var mu sync.Mutex
	var available, executed, lmEnd []interface{}
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
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "echo", Arguments: map[string]interface{}{"v": "raw"}}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	tool := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"v": map[string]interface{}{"type": "string"}}},
		OnInputAvailable: func(_ context.Context, o types.OnInputAvailableOptions) error {
			mu.Lock()
			defer mu.Unlock()
			available = append(available, o.Input["v"])
			return nil
		},
		Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, input["v"])
			return "ok", nil
		},
	}
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{tool},
		ExperimentalRefineToolInput: map[string]ai.ToolInputRefiner{
			"echo": func(_ context.Context, o ai.ToolInputRefinementOptions) (map[string]interface{}, error) {
				return map[string]interface{}{"v": "refined"}, nil
			},
		},
		OnLanguageModelCallEnd: func(_ context.Context, e ai.LanguageModelCallEndEvent) {
			mu.Lock()
			defer mu.Unlock()
			for _, part := range e.Content {
				if tc, ok := part.(types.ToolCallContent); ok {
					lmEnd = append(lmEnd, tc.Arguments["v"])
				}
			}
		},
	})
	if _, err := agent.Execute(context.Background(), "hi"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(available) != 1 || available[0] != "refined" {
		t.Fatalf("OnInputAvailable inputs = %v, want [refined]", available)
	}
	if len(executed) != 1 || executed[0] != "refined" {
		t.Fatalf("executed inputs = %v, want [refined]", executed)
	}
	if len(lmEnd) != 1 || lmEnd[0] != "refined" {
		t.Fatalf("language-model-call end tool inputs = %v, want [refined]", lmEnd)
	}
}
