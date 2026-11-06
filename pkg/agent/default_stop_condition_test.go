package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS default-stop-condition.test.ts (#22329).
func captureWarnings(t *testing.T) func() []ai.LogWarningsOptions {
	t.Helper()
	var mu sync.Mutex
	var got []ai.LogWarningsOptions
	ai.SetLogWarnings(func(o ai.LogWarningsOptions) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, o)
	})
	t.Cleanup(func() { ai.SetLogWarnings(nil) })
	return func() []ai.LogWarningsOptions {
		mu.Lock()
		defer mu.Unlock()
		return append([]ai.LogWarningsOptions(nil), got...)
	}
}

func toolLoopResponses(n int, finishAt int) []types.GenerateResult {
	responses := make([]types.GenerateResult, n)
	for i := range responses {
		if i+1 == finishAt {
			responses[i] = types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop, Usage: types.Usage{TotalTokens: intPtr(1)}}
			continue
		}
		responses[i] = types.GenerateResult{
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: fmt.Sprintf("call-%d", i+1), ToolName: "test_tool", Arguments: map[string]interface{}{}}},
			Usage:        types.Usage{TotalTokens: intPtr(1)},
		}
	}
	return responses
}

func defaultStopTestTool() types.Tool {
	return types.Tool{
		Name:       "test_tool",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result", nil
		},
	}
}

func TestToolLoopAgent_DefaultStopCondition_WarnsWhenLimitStopsLoop(t *testing.T) {
	warnings := captureWarnings(t)
	agent := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{responses: toolLoopResponses(25, 0)}, Tools: []types.Tool{defaultStopTestTool()}})
	result, err := agent.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 20 {
		t.Fatalf("steps = %d", len(result.Steps))
	}
	got := warnings()
	if len(got) != 1 || len(got[0].Warnings) != 1 {
		t.Fatalf("warnings = %#v", got)
	}
	w := got[0].Warnings[0]
	if w.Type != "other" || !strings.Contains(w.Message, "IsStepCount(20)") || !strings.Contains(w.Message, "StopWhen") {
		t.Fatalf("warning = %#v", w)
	}
	if got[0].Provider == "" || got[0].Model == "" {
		t.Fatalf("expected provider/model scope, got %q / %q", got[0].Provider, got[0].Model)
	}
}

func TestToolLoopAgent_DefaultStopCondition_NoWarningWhenModelFinishesNaturally(t *testing.T) {
	for _, step := range []int{1, 20} {
		warnings := captureWarnings(t)
		agent := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{responses: toolLoopResponses(20, step)}, Tools: []types.Tool{defaultStopTestTool()}})
		result, err := agent.Execute(context.Background(), "go")
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Steps) != step || len(warnings()) != 0 {
			t.Fatalf("finishAt %d: steps = %d, warnings = %#v", step, len(result.Steps), warnings())
		}
	}
}

func TestToolLoopAgent_DefaultStopCondition_NoWarningForExplicitLimit(t *testing.T) {
	warnings := captureWarnings(t)
	agent := NewToolLoopAgent(AgentConfig{
		Model:    &mockLanguageModel{responses: toolLoopResponses(25, 0)},
		Tools:    []types.Tool{defaultStopTestTool()},
		StopWhen: []ai.StopCondition{ai.IsStepCount(20)},
	})
	result, err := agent.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 20 || len(warnings()) != 0 {
		t.Fatalf("steps = %d, warnings = %#v", len(result.Steps), warnings())
	}
}
