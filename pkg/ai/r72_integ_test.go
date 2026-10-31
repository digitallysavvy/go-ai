package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r72 stream CR-1: GenerateText's step-end event is the StepResult (TS
// GenerateTextStepEndEvent = StepResult: content, performance and
// request.messages), and the end event's request is the final step's.
func TestR72Integ_GenerateTextStepEventCarriesContentPerformanceRequest(t *testing.T) {
	var mu sync.Mutex
	var events []OnStepFinishEvent
	var end OnFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
		Include:  &IncludeOptions{RequestMessages: true},
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			end = e
		},
	})
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
	if _, ok := events[0].Content[1].(types.ToolResultContent); !ok {
		t.Errorf("step 0 content = %#v, want a tool result second", events[0].Content)
	}
	if len(end.Request.Messages) == 0 || !reflect.DeepEqual(end.Request.Messages, res.Steps[1].Request.Messages) {
		t.Errorf("onEnd request messages = %#v, want the final step's", end.Request.Messages)
	}
}
