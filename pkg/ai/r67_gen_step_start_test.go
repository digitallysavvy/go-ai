package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r67StepStartTools() []types.Tool {
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}
	return []types.Tool{
		{Name: "a", Parameters: map[string]interface{}{"type": "object"}, Execute: exec},
		{Name: "b", Parameters: map[string]interface{}{"type": "object"}, Execute: exec},
	}
}

// TestR67StepStartEventCarriesCallToolsAndOutput: TS stepStartEvent.tools is
// the call's tools (ActiveTools narrows them for the step, the model gets
// the narrowed set) and stepStartEvent.output the output specification.
// Both GenerateText steps and both StreamText step paths (first and next).
func TestR67StepStartEventCarriesCallToolsAndOutput(t *testing.T) {
	output := TextOutput()
	check := func(label string, events []OnStepStartEvent, modelTools [][]types.Tool) {
		t.Helper()
		if len(events) != 2 {
			t.Fatalf("%s: %d step start events, want 2", label, len(events))
		}
		for i, e := range events {
			if len(e.Tools) != 2 || e.Tools[0].Name != "a" || e.Tools[1].Name != "b" {
				t.Fatalf("%s step %d: Tools = %v, want the call's tools [a b]", label, i, toolNames(e.Tools))
			}
			if e.Output == nil {
				t.Fatalf("%s step %d: Output not set", label, i)
			}
		}
		for i, tools := range modelTools {
			if len(tools) != 1 || tools[0].Name != "a" {
				t.Fatalf("%s model call %d tools = %v, want [a] (ActiveTools)", label, i, toolNames(tools))
			}
		}
	}

	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}}}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	var genEvents []OnStepStartEvent
	var genModelTools [][]types.Tool
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "hi", Tools: r67StepStartTools(), ActiveTools: []string{"a"},
		StopWhen: []StopCondition{IsStepCount(3)}, Output: output,
		OnStepStart:              func(_ context.Context, e OnStepStartEvent) { genEvents = append(genEvents, e) },
		OnLanguageModelCallStart: func(_ context.Context, e LanguageModelCallStartEvent) { genModelTools = append(genModelTools, e.Tools) },
	}); err != nil {
		t.Fatal(err)
	}
	check("GenerateText", genEvents, genModelTools)

	streamCalls := 0
	streamModel := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			streamCalls++
			if streamCalls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "a", Arguments: map[string]interface{}{}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var mu sync.Mutex
	var streamEvents []OnStepStartEvent
	var streamModelTools [][]types.Tool
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: streamModel, Prompt: "hi", Tools: r67StepStartTools(), ActiveTools: []string{"a"},
		StopWhen: []StopCondition{IsStepCount(3)}, Output: output,
		OnStepStart: func(_ context.Context, e OnStepStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			streamEvents = append(streamEvents, e)
		},
		OnLanguageModelCallStart: func(_ context.Context, e LanguageModelCallStartEvent) {
			mu.Lock()
			defer mu.Unlock()
			streamModelTools = append(streamModelTools, e.Tools)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Text()
	mu.Lock()
	defer mu.Unlock()
	check("StreamText", streamEvents, streamModelTools)
}
