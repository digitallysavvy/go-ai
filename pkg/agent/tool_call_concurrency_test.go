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
	run := func(t *testing.T, concurrency *int, gate int) []string {
		t.Helper()
		var mu sync.Mutex
		var events []string
		// The first gate executions wait for each other, so overlap is
		// proven by rendezvous instead of timing.
		arrived := 0
		opened := make(chan struct{})
		mkTool := func(name string) types.Tool {
			return types.Tool{
				Name:       name,
				Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					mu.Lock()
					events = append(events, name+":start")
					mu.Unlock()
					if gate > 0 {
						mu.Lock()
						arrived++
						if arrived == gate {
							close(opened)
						}
						mu.Unlock()
						select {
						case <-opened:
						case <-time.After(10 * time.Second):
						}
					}
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
	if got := run(t, &one, 0); !reflect.DeepEqual(got, []string{"first:start", "first:end", "second:start", "second:end"}) {
		t.Fatalf("concurrency 1 events = %v", got)
	}
	two := 2
	got := run(t, &two, 2)
	if len(got) != 4 {
		t.Fatalf("concurrency 2 events = %v", got)
	}
	if unset := run(t, nil, 2); len(unset) != 4 || unset[0][len(unset[0])-5:] != "start" || unset[1][len(unset[1])-5:] != "start" {
		t.Fatalf("unset concurrency should run both calls concurrently: %v", unset)
	}
	if (got[0] != "first:start" || got[1] != "second:start") && (got[0] != "second:start" || got[1] != "first:start") {
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
