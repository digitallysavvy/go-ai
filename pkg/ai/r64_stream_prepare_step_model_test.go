package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText gives every step's prepareStep the call's model and uses
// `prepareStepResult?.model ?? model`, so a model PrepareStep returns for one
// step does not carry over to the next, as in GenerateText.
func TestR64StreamPrepareStepModelDoesNotCarryOver(t *testing.T) {
	var mu sync.Mutex
	var used []string
	newModel := func(name string) *testutil.MockLanguageModel {
		calls := 0
		return &testutil.MockLanguageModel{ModelName: name, DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			used = append(used, name)
			calls++
			mu.Unlock()
			if name == "override" {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
	}
	callModel, override := newModel("call"), newModel("override")
	var prepared []string
	res, err := StreamText(context.Background(), StreamTextOptions{
		Prompt:   "a",
		Model:    callModel,
		StopWhen: []StopCondition{StepCountIs(3)},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepared = append(prepared, s.Model.ModelID())
			mu.Unlock()
			if s.StepNumber == 0 {
				return PrepareStepOptions{Model: override}
			}
			return PrepareStepOptions{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(used) != 2 || used[0] != "override" || used[1] != "call" {
		t.Fatalf("models used = %v, want [override call]", used)
	}
	if len(prepared) != 2 || prepared[1] != callModel.ModelID() {
		t.Fatalf("PrepareStep models = %v, want the call's model at step 1", prepared)
	}
	if got := res.Steps()[1].Model.ModelID; got != callModel.ModelID() {
		t.Fatalf("step 1 model = %q, want %q", got, callModel.ModelID())
	}
}
