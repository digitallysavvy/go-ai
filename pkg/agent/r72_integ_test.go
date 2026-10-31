package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r72 stream CR-2: the legacy loop's step-end event carries the step's
// content, performance and request messages, and the end event the final
// step's request messages (TS onStepEnd/onEnd receive the StepResult).
func TestR72Integ_LegacyStepEventCarriesContentPerformanceRequest(t *testing.T) {
	mock := &mockLanguageModel{responses: []types.GenerateResult{{
		ToolCalls:    []types.ToolCall{{ID: "c-1", ToolName: "ok", Arguments: map[string]interface{}{}}},
		FinishReason: types.FinishReasonToolCalls,
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	var mu sync.Mutex
	var events []ai.OnStepFinishEvent
	var end ai.OnFinishEvent
	a := NewToolLoopAgent(AgentConfig{Model: mock, MaxSteps: 2,
		Include: &ai.IncludeOptions{RequestMessages: true},
		OnStepEndEvent: func(_ context.Context, e ai.OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		},
		OnEndEvent: func(_ context.Context, e ai.OnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			end = e
		},
		Tools: []types.Tool{{Name: "ok", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "fine", nil
		}}}})
	res, err := a.Execute(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || len(res.Steps) != 2 {
		t.Fatalf("events = %d, steps = %d", len(events), len(res.Steps))
	}
	for i, e := range events {
		if len(e.Content) == 0 || !reflect.DeepEqual(e.Content, res.Steps[i].Content) {
			t.Errorf("event %d content = %#v, want %#v", i, e.Content, res.Steps[i].Content)
		}
		if !reflect.DeepEqual(e.Performance, res.Steps[i].Performance) {
			t.Errorf("event %d performance = %#v, want %#v", i, e.Performance, res.Steps[i].Performance)
		}
		if len(e.Request.Messages) == 0 || !reflect.DeepEqual(e.Request.Messages, res.Steps[i].Request.Messages) {
			t.Errorf("event %d request messages = %#v, want %#v", i, e.Request.Messages, res.Steps[i].Request.Messages)
		}
	}
	if len(end.Request.Messages) == 0 || !reflect.DeepEqual(end.Request.Messages, res.Steps[1].Request.Messages) {
		t.Errorf("onEnd request messages = %#v, want the final step's", end.Request.Messages)
	}
}
