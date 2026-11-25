package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText, streamText and startBatch pass the model
// prepareToolChoice(toolChoice): an unset tool choice reaches doGenerate and
// doStream as {type: 'auto'} (generate-text.test.ts "should contain tool
// results": expect(toolChoice).toStrictEqual({type: 'auto'})), with or
// without tools. The step start event keeps the choice as given.
func TestR63UnsetToolChoiceReachesTheModelAsAuto(t *testing.T) {
	tool := types.Tool{Name: "a", Parameters: map[string]interface{}{"type": "object"}}
	for _, tc := range []struct {
		name  string
		tools []types.Tool
		set   types.ToolChoice
		want  types.ToolChoice
	}{
		{"tools", []types.Tool{tool}, types.ToolChoice{}, types.AutoToolChoice()},
		{"no tools", nil, types.ToolChoice{}, types.AutoToolChoice()},
		{"required", []types.Tool{tool}, types.RequiredToolChoice(), types.RequiredToolChoice()},
	} {
		var mu sync.Mutex
		var got []types.ToolChoice
		record := func(o *provider.GenerateOptions) {
			mu.Lock()
			got = append(got, o.ToolChoice)
			mu.Unlock()
		}
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
				record(o)
				if tc.set.Type == types.ToolChoiceRequired {
					return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls}, nil
				}
				return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
			},
			DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
				record(o)
				if tc.set.Type == types.ToolChoiceRequired {
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				}
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "ok"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		}
		var stepChoices []types.ToolChoice
		onStepStart := func(_ context.Context, e OnStepStartEvent) {
			mu.Lock()
			stepChoices = append(stepChoices, e.ToolChoice)
			mu.Unlock()
		}
		if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "go", Tools: tc.tools, ToolChoice: tc.set, StopWhen: []StopCondition{StepCountIs(1)}, OnStepStart: onStepStart,
		}); err != nil {
			t.Fatal(err)
		}
		st, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Prompt: "go", Tools: tc.tools, ToolChoice: tc.set, StopWhen: []StopCondition{StepCountIs(1)}, OnStepStart: onStepStart,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.ReadAll(); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if len(got) != 2 || got[0] != tc.want || got[1] != tc.want {
			t.Errorf("%s: model tool choices (generate, stream) = %+v, want %+v twice", tc.name, got, tc.want)
		}
		for _, c := range stepChoices {
			if c != tc.set {
				t.Errorf("%s: step start ToolChoice = %+v, want the choice as given %+v", tc.name, c, tc.set)
			}
		}
		mu.Unlock()
	}

	var batchChoice types.ToolChoice
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock", startFn: func(_ context.Context, o provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			batchChoice = o.Requests[0].Text.Options.ToolChoice
			return &provider.BatchV4StartResult{BatchID: "b1"}, nil
		}},
		Requests: []BatchRequest{{Text: &BatchTextRequest{ID: "r1", Model: "m", Prompt: "go", Tools: []types.Tool{tool}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if batchChoice != types.AutoToolChoice() {
		t.Errorf("batch request tool choice = %+v, want auto", batchChoice)
	}
}
