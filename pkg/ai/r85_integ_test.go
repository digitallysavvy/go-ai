package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r85integText(text string, reason types.FinishReason) []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: text},
		{Type: provider.ChunkTypeFinish, FinishReason: reason, Usage: &types.Usage{}},
	}
}

func r85integLastUserTexts(prompts [][]string) []string {
	out := make([]string, len(prompts))
	for i, p := range prompts {
		if len(p) > 0 {
			out[i] = p[len(p)-1]
		}
	}
	return out
}

func r85integStream(t *testing.T, opts StreamTextOptions) *StreamTextResult {
	t.Helper()
	result, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatal(err)
	}
	return result
}

// Ports of stream-text-steering.test.ts 'queue' (TS cf2796985a,
// stream-text.ts 3392-3400): a queued message waits for the current task,
// one per task in order, never after a length / content-filter finish, and
// not when the default stop condition ends the stream.
func TestR85integStreamTextSteeringQueue(t *testing.T) {
	t.Run("waits until the current task is done", func(t *testing.T) {
		steering := NewSteering()
		var handle *SteerHandle
		var mu sync.Mutex
		var prompts [][]string
		model := steerStreamModel(func(call int) []provider.StreamChunk {
			switch call {
			case 1:
				return []provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "weather", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{}},
				}
			case 2:
				return r85integText("It is sunny in Paris.", types.FinishReasonStop)
			}
			return r85integText("The readme is updated.", types.FinishReasonStop)
		}, &prompts, &mu)
		tools := []types.Tool{{Name: "weather", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			handle = steering.Queue("Then update the readme.")
			return "sunny", nil
		}}}
		result := r85integStream(t, StreamTextOptions{
			Model: model, Prompt: "What is the weather in Paris?", Tools: tools,
			StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
		})
		want := []string{"What is the weather in Paris?", "What is the weather in Paris?", "Then update the readme."}
		if got := r85integLastUserTexts(prompts); !reflect.DeepEqual(got, want) {
			t.Fatalf("last user texts = %q, want %q", got, want)
		}
		if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 2 {
			t.Fatalf("outcome = %+v", o)
		}
		if got := result.Text(); got != "The readme is updated." {
			t.Fatalf("text = %q", got)
		}
	})

	t.Run("one queued message per task in order", func(t *testing.T) {
		steering := NewSteering()
		first := steering.Queue("Then update the readme.")
		second := steering.Queue("Then add a changelog entry.")
		answers := []string{"It is sunny in Paris.", "The readme is updated.", "The changelog is updated."}
		var mu sync.Mutex
		var prompts [][]string
		model := steerStreamModel(func(call int) []provider.StreamChunk {
			return r85integText(answers[call-1], types.FinishReasonStop)
		}, &prompts, &mu)
		result := r85integStream(t, StreamTextOptions{
			Model: model, Prompt: "What is the weather in Paris?",
			StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
		})
		want := []string{"What is the weather in Paris?", "Then update the readme.", "Then add a changelog entry."}
		if got := r85integLastUserTexts(prompts); !reflect.DeepEqual(got, want) {
			t.Fatalf("last user texts = %q, want %q", got, want)
		}
		if o := waitOutcome(t, first); !o.Delivered || o.StepNumber != 1 {
			t.Fatalf("first = %+v", o)
		}
		if o := waitOutcome(t, second); !o.Delivered || o.StepNumber != 2 {
			t.Fatalf("second = %+v", o)
		}
		steps := result.Steps()
		if len(steps) != 3 || len(steps[0].ExperimentalSteeringMessages) != 0 ||
			len(steps[1].ExperimentalSteeringMessages) != 1 || userText(steps[1].ExperimentalSteeringMessages[0]) != "Then update the readme." ||
			len(steps[2].ExperimentalSteeringMessages) != 1 || userText(steps[2].ExperimentalSteeringMessages[0]) != "Then add a changelog entry." {
			t.Fatalf("steps = %+v", steps)
		}
	})

	for _, reason := range []types.FinishReason{types.FinishReasonLength, types.FinishReasonContentFilter} {
		t.Run("not after "+string(reason), func(t *testing.T) {
			steering := NewSteering()
			h := steering.Queue("Then update the readme.")
			var mu sync.Mutex
			var prompts [][]string
			model := steerStreamModel(func(int) []provider.StreamChunk {
				return r85integText("It is sunny", reason)
			}, &prompts, &mu)
			r85integStream(t, StreamTextOptions{
				Model: model, Prompt: "What is the weather in Paris?",
				StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
			})
			if len(prompts) != 1 {
				t.Fatalf("model calls = %d, want 1", len(prompts))
			}
			if o := waitOutcome(t, h); o.Delivered || o.Reason != SteeringFinished {
				t.Fatalf("outcome = %+v", o)
			}
		})
	}

	t.Run("default stop condition ends the stream", func(t *testing.T) {
		steering := NewSteering()
		h := steering.Queue("Then update the readme.")
		var mu sync.Mutex
		var prompts [][]string
		model := steerStreamModel(func(int) []provider.StreamChunk {
			return r85integText("It is sunny in Paris.", types.FinishReasonStop)
		}, &prompts, &mu)
		r85integStream(t, StreamTextOptions{Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering})
		if len(prompts) != 1 {
			t.Fatalf("model calls = %d, want 1", len(prompts))
		}
		if o := waitOutcome(t, h); o.Delivered || o.Reason != SteeringStopCondition {
			t.Fatalf("outcome = %+v", o)
		}
	})
}
