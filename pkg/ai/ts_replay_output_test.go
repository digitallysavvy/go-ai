package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays stream-text.test.ts 'options.output': structured output after an
// earlier tool step emitted text (only the final step's text is parsed), onEnd
// with a failed parse, and choice/json/array outputs.

type valueObj struct {
	Value string `json:"value"`
}

func toolThenAnswerModel(introID, answer string) *testutil.MockLanguageModel {
	var mu sync.Mutex
	calls := 0
	return &testutil.MockLanguageModel{
		StructuredSupport: true,
		ToolSupport:       true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: introID},
					{Type: provider.ChunkTypeText, ID: introID, Text: "Checking the value."},
					{Type: provider.ChunkTypeTextEnd, ID: introID},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "lookup", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: usage(3, 10)},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "answer"},
				{Type: provider.ChunkTypeText, ID: "answer", Text: answer},
				{Type: provider.ChunkTypeTextEnd, ID: "answer"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)},
			}), nil
		},
	}
}

func lookupTool() types.Tool {
	return types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		},
	}
}

func TestTSReplay_StreamText_ObjectOutputAfterToolStep(t *testing.T) {
	var ended *OnFinishEvent
	var mu sync.Mutex
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  toolThenAnswerModel("intro", `{"value":"done"}`),
		Prompt: "Look up the value and return it.",
		Tools:  []types.Tool{lookupTool()},
		Output: ObjectOutput[valueObj](ObjectOutputOptions{
			Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}),
		}),
		StopWhen:   []StopCondition{IsStepCount(2)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); ended = &e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.Output(); !reflect.DeepEqual(got, valueObj{Value: "done"}) {
		t.Fatalf("output = %#v (err %v)", got, res.OutputErr())
	}
	if got := res.PartialOutput(); !reflect.DeepEqual(got, valueObj{Value: "done"}) && !reflect.DeepEqual(got, map[string]interface{}{"value": "done"}) {
		t.Fatalf("partial output = %#v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if ended == nil || !reflect.DeepEqual(ended.Output, valueObj{Value: "done"}) {
		t.Fatalf("onEnd output = %#v", ended)
	}
}

func TestTSReplay_StreamText_ArrayOutputAfterToolStep(t *testing.T) {
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  toolThenAnswerModel("answer", `{"elements":[{"value":"done"}]}`),
		Prompt: "Look up the value and return it.",
		Tools:  []types.Tool{lookupTool()},
		Output: ArrayOutput[valueObj](ArrayOutputOptions[valueObj]{
			ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}),
		}),
		StopWhen: []StopCondition{IsStepCount(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := res.Output(); !reflect.DeepEqual(got, []valueObj{{Value: "done"}}) {
		t.Fatalf("output = %#v (err %v)", got, res.OutputErr())
	}
}

func TestTSReplay_StreamText_OnEndOutputUndefinedWhenParseFails(t *testing.T) {
	var ended *OnFinishEvent
	var mu sync.Mutex
	model := toolThenAnswerModel("intro", `not json`)
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "p", Tools: []types.Tool{lookupTool()},
		Output: ObjectOutput[valueObj](ObjectOutputOptions{
			Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}}),
		}),
		StopWhen:   []StopCondition{IsStepCount(2)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); ended = &e; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = res.ReadAll()
	if res.OutputErr() == nil {
		t.Fatal("expected a parse error from OutputErr")
	}
	mu.Lock()
	defer mu.Unlock()
	if ended == nil || ended.Output != nil {
		t.Fatalf("onEnd = %#v, want output nil", ended)
	}
}
