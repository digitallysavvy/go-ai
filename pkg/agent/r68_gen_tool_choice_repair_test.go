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

// TS generateText checks the enforced tool choice against the parsed tool
// calls (stepToolCalls), so a call that repairToolCall renamed to the forced
// tool satisfies it. The AgentResult loop checked the raw calls and failed
// with a ToolChoiceViolationError.
func TestR68LegacyLoopToolChoiceChecksRepairedCalls(t *testing.T) {
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
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "wether", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var executed bool
	agent := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{
			Name: "weather",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				defer mu.Unlock()
				executed = true
				return "sunny", nil
			},
		}},
		ToolChoice: types.SpecificToolChoice("weather"),
		RepairToolCall: func(_ context.Context, o ai.ToolCallRepairOptions) (*types.ToolCall, error) {
			repaired := o.ToolCall
			repaired.ToolName = "weather"
			return &repaired, nil
		},
		StopWhen: []ai.StopCondition{ai.IsStepCount(1)},
	})
	if _, err := agent.Execute(context.Background(), "q"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !executed {
		t.Fatal("the repaired call did not run")
	}

	// GenerateText (the TS twin) accepts the same response.
	calls = 0
	executed = false
	mu.Unlock()
	_, err := agent.Generate(context.Background(), AgentGenerateOptions{Prompt: "q"})
	mu.Lock()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
}
