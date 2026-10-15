package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
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

func TestLegacyLoopRejectsMissingToolResults(t *testing.T) {
	model := &mockLanguageModel{}
	a := NewToolLoopAgent(AgentConfig{Model: model})
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: "{}"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
	}
	_, err := a.ExecuteWithMessages(context.Background(), msgs)
	var missing *types.MissingToolResultsError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want MissingToolResultsError", err)
	}
	if model.callCount != 0 || len(model.options) != 0 {
		t.Fatal("model must not be called")
	}
}

func TestLegacyLoopStopsOnCancelDuringConcurrentTools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	one := 1
	var ran int32
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(&ran, 1)
			cancel()
			return "ok", nil
		}}
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls: []types.ToolCall{
			{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}},
			{ID: "2", ToolName: "t", Arguments: map[string]interface{}{}},
		},
	}}}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{tool}, ToolCallConcurrency: &one})
	_, err := a.Execute(ctx, "go")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if atomic.LoadInt32(&ran) != 1 {
		t.Fatalf("ran = %d, want 1", ran)
	}
}

func TestLegacyLoopPausesOnUnresolvedLocalToolCall(t *testing.T) {
	withExec := types.Tool{Name: "a", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}
	noExec := types.Tool{Name: "b", Parameters: map[string]interface{}{"type": "object"}}
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls: []types.ToolCall{
			{ID: "1", ToolName: "a", Arguments: map[string]interface{}{}},
			{ID: "2", ToolName: "b", Arguments: map[string]interface{}{}},
		},
	}}}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{withExec, noExec}})
	res, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || model.callCount != 1 {
		t.Fatalf("steps=%d calls=%d, want loop to pause after 1 step", len(res.Steps), model.callCount)
	}
}
