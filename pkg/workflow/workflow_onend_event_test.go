package workflow

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent onEnd event: steps, messages (the final conversation),
// text, finishReason, usage == totalUsage, runtimeContext, toolsContext and
// output. Both modes must deliver the same shape.
func TestWorkflowOnEndEventCarriesFinalMessagesAndTotals(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var got []ai.OnFinishEvent
			model := &scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{"x": "a"}}}}
			tool := types.Tool{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "r", nil
			}}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Tools: []types.Tool{tool}, RuntimeContext: map[string]interface{}{"k": "v"},
				OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); got = append(got, e); mu.Unlock() },
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
			if len(got) != 1 {
				t.Fatalf("onEnd calls = %d, want 1", len(got))
			}
			e := got[0]
			if len(e.Steps) != 2 {
				t.Fatalf("steps = %d, want 2", len(e.Steps))
			}
			// messages = user prompt + each step's response messages.
			want := 1
			for _, s := range e.Steps {
				want += len(s.Response.Messages)
			}
			if want < 3 || len(e.Messages) != want {
				t.Fatalf("messages = %d, want %d (>=3)", len(e.Messages), want)
			}
			if e.Messages[0].Role != types.RoleUser {
				t.Fatalf("first message role = %q, want user", e.Messages[0].Role)
			}
			if len(e.ToolCalls) != 1 || len(e.ToolResults) != 1 {
				t.Fatalf("aggregated toolCalls/results = %d/%d, want 1/1 (all steps)", len(e.ToolCalls), len(e.ToolResults))
			}
			if !reflect.DeepEqual(e.Usage, e.TotalUsage) {
				t.Fatalf("usage %+v != totalUsage %+v", e.Usage, e.TotalUsage)
			}
			if e.Text != e.Steps[1].Text {
				t.Fatalf("text = %q, want last step text %q", e.Text, e.Steps[1].Text)
			}
		})
	}
}

func TestWorkflowOnEndMessagesIncludeInputMessages(t *testing.T) {
	var got ai.OnFinishEvent
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: &scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}},
		Tools: []types.Tool{{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		OnEnd: func(_ context.Context, e ai.OnFinishEvent) { got = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	input := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "c"}}},
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: input}); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 6 || got.Messages[2].Role != types.RoleUser || got.Messages[3].Role != types.RoleAssistant || got.Messages[4].Role != types.RoleTool {
		t.Fatalf("messages = %+v", got.Messages)
	}
}

// TS notifyEnd: generate mode skips onEnd when the run failed; stream mode
// (and every aborted run) differ: stream still reports the end, abort never.
func TestWorkflowOnEndFiringOnFailureAndAbort(t *testing.T) {
	var ends int
	onEnd := func(context.Context, ai.OnFinishEvent) { ends++ }

	w, _ := NewWorkflowAgent(WorkflowAgent{Model: &wfErrorModel{}, OnEnd: onEnd})
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err == nil {
		t.Fatal("expected generate error")
	}
	if ends != 0 {
		t.Fatalf("generate failure fired onEnd %d times, want 0", ends)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w2, _ := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}, OnEnd: onEnd})
	_, _ = w2.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi"})
	if ends != 0 {
		t.Fatalf("aborted run fired onEnd %d times, want 0", ends)
	}

	w3, _ := NewWorkflowAgent(WorkflowAgent{Model: &errChunkModel{}, OnEnd: onEnd})
	res, err := w3.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.ConsumeStream()
	if ends != 1 {
		t.Fatalf("stream with a terminal model error fired onEnd %d times, want 1", ends)
	}
}
