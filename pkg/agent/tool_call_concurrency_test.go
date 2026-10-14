package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestToolLoopAgent_ToolCallConcurrency(t *testing.T) {
	run := func(t *testing.T, concurrency *int) []string {
		t.Helper()
		var mu sync.Mutex
		var events []string
		mkTool := func(name string) types.Tool {
			return types.Tool{
				Name:       name,
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					mu.Lock()
					events = append(events, name+":start")
					mu.Unlock()
					time.Sleep(20 * time.Millisecond)
					mu.Lock()
					events = append(events, name+":end")
					mu.Unlock()
					return name, nil
				},
			}
		}
		model := &mockLanguageModel{responses: []types.GenerateResult{{
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls: []types.ToolCall{
				{ID: "1", ToolName: "first", Arguments: map[string]interface{}{}},
				{ID: "2", ToolName: "second", Arguments: map[string]interface{}{}},
			},
		}, {Text: "done", FinishReason: types.FinishReasonStop}}}
		a := NewToolLoopAgent(AgentConfig{
			Model: model, Tools: []types.Tool{mkTool("first"), mkTool("second")},
			ToolCallConcurrency: concurrency, StopWhen: []ai.StopCondition{ai.IsStepCount(2)},
		})
		if _, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	one := 1
	if got := run(t, &one); !reflect.DeepEqual(got, []string{"first:start", "first:end", "second:start", "second:end"}) {
		t.Fatalf("concurrency 1 events = %v", got)
	}
	two := 2
	got := run(t, &two)
	if len(got) != 4 || got[1] != "second:start" && got[1] != "first:end" {
		t.Fatalf("concurrency 2 events = %v", got)
	}
	if !(got[0] != "" && ((got[0] == "first:start" && got[1] == "second:start") || (got[0] == "second:start" && got[1] == "first:start"))) {
		t.Fatalf("concurrency 2 should start both calls before either ends: %v", got)
	}
}

func TestToolLoopAgent_ToolCallConcurrencyRejectsInvalid(t *testing.T) {
	zero := 0
	a := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{}, ToolCallConcurrency: &zero})
	if _, err := a.Generate(context.Background(), AgentGenerateOptions{Prompt: "go"}); err == nil {
		t.Fatal("expected an error for ToolCallConcurrency 0")
	}
}
