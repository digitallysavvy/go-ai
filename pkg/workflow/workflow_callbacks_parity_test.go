package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestWorkflowGenerateAndStreamFireSameCallbacks(t *testing.T) {
	run := func(stream bool) []string {
		var mu sync.Mutex
		var log []string
		add := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
		model := &scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{"x": "a"}}}}
		tool := types.Tool{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}
		w, err := NewWorkflowAgent(WorkflowAgent{
			Model: model, Tools: []types.Tool{tool}, RuntimeContext: map[string]interface{}{"k": "v"},
			OnStart:     func(_ context.Context, e ai.OnStartEvent) { add("start") },
			OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) { add(fmt.Sprintf("stepStart:%d", e.StepNumber)) },
			OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
				add("toolStart:" + e.ToolName + ":" + e.ToolCallID)
			},
			OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
				add("toolEnd:" + e.ToolName + ":" + fmt.Sprint(e.ToolOutput.Result))
			},
			OnStepEnd: func(_ context.Context, e ai.OnStepFinishEvent) {
				add(fmt.Sprintf("stepEnd:%d:%s:%d", e.StepNumber, e.FinishReason, len(e.ToolCalls)))
			},
			OnEnd: func(_ context.Context, e ai.OnFinishEvent) {
				add(fmt.Sprintf("end:%d:%s", len(e.Steps), e.FinishReason))
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			_ = res.ConsumeStream()
		} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
			t.Fatal(err)
		}
		return log
	}
	g, s := run(false), run(true)
	if fmt.Sprint(g) != fmt.Sprint(s) {
		t.Fatalf("callbacks differ\n gen: %v\n str: %v", g, s)
	}
	t.Logf("%v", g)
}
