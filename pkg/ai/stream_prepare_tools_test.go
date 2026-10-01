package ai

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestStreamText_DescriptionFuncResolvedAfterPrepareStep(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			if len(opts.Tools) != 1 {
				t.Fatalf("tools count = %d, want 1", len(opts.Tools))
			}
			if got, want := opts.Tools[0].Description, "ctx=prepared-tool,sbx=prepared-sbx"; got != want {
				t.Fatalf("tool description = %q, want %q", got, want)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	tool := types.Tool{
		Name:       "weather",
		Parameters: map[string]interface{}{"type": "object"},
		DescriptionFunc: func(_ context.Context, options types.ToolDescriptionOptions) string {
			return fmt.Sprintf("ctx=%v,sbx=%v", options.Context, options.ExperimentalSandbox)
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:               model,
		Prompt:              "hi",
		Tools:               []types.Tool{tool},
		RuntimeContext:      "initial",
		ToolsContext:        map[string]interface{}{"weather": "initial-tool"},
		ExperimentalSandbox: "initial-sbx",
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{
				RuntimeContext:      "prepared",
				ToolsContext:        map[string]interface{}{"weather": "prepared-tool"},
				ExperimentalSandbox: "prepared-sbx",
			}
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
}

func TestStreamText_ToolOrderAppendsUnlistedAlphabetically(t *testing.T) {
	t.Parallel()

	var got []string
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			for _, tool := range opts.Tools {
				got = append(got, tool.Name)
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{
			{Name: "zebra", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "alpha", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "middle", Parameters: map[string]interface{}{"type": "object"}},
		},
		ToolOrder: []string{"middle"},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	want := []string{"middle", "alpha", "zebra"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tool order = %v, want %v", got, want)
	}
}

func TestStreamText_OnStepFinishEventFiresPerStep(t *testing.T) {
	t.Parallel()

	callCount := 0
	stepStartCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID: "tc1", ToolName: "echo", Arguments: map[string]interface{}{"value": "a"},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	tool := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	stepFinishCount := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		Tools:  []types.Tool{tool},
		OnStepStart: func(_ context.Context, _ OnStepStartEvent) {
			stepStartCount++
		},
		OnStepFinishEvent: func(_ context.Context, _ OnStepFinishEvent) {
			stepFinishCount++
		},
		OnFinishEvent: func(_ context.Context, _ OnFinishEvent) {},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	steps := result.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	if stepFinishCount != 2 {
		t.Fatalf("OnStepFinishEvent count = %d, want 2", stepFinishCount)
	}
	if stepStartCount != 2 {
		t.Fatalf("OnStepStart count = %d, want 2", stepStartCount)
	}
}

func TestStreamText_OnStepEndEventTakesPrecedenceOverDeprecatedOnStepFinishEvent(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	var stepEndCalls int
	var stepFinishCalls int
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		OnStepEndEvent: func(_ context.Context, _ OnStepFinishEvent) {
			stepEndCalls++
		},
		OnStepFinishEvent: func(_ context.Context, _ OnStepFinishEvent) {
			stepFinishCalls++
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	_ = result.Steps()
	if stepEndCalls != 1 || stepFinishCalls != 0 {
		t.Fatalf("callbacks: OnStepEndEvent=%d OnStepFinishEvent=%d", stepEndCalls, stepFinishCalls)
	}
}

func TestStreamText_OnStepStartOnlyStartsProcessingLoop(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID: "tc1", ToolName: "echo", Arguments: map[string]interface{}{"value": "a"},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	tool := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	stepStartCount := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		Tools:  []types.Tool{tool},
		OnStepStart: func(_ context.Context, _ OnStepStartEvent) {
			stepStartCount++
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	steps := result.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	if stepStartCount != 2 {
		t.Fatalf("OnStepStart count = %d, want 2", stepStartCount)
	}
}

func TestStreamText_ToolExecutionCallbacksOnlyStartProcessingLoop(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID: "tc1", ToolName: "echo", Arguments: map[string]interface{}{"value": "a"},
				}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}

	tool := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	startCount := 0
	finishCount := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		Tools:  []types.Tool{tool},
		StopWhen: []StopCondition{
			StepCountIs(1),
		},
		OnToolExecutionStart: func(_ context.Context, _ OnToolCallStartEvent) {
			startCount++
		},
		OnToolExecutionEnd: func(_ context.Context, _ OnToolCallFinishEvent) {
			finishCount++
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	if got := len(result.ToolResults()); got != 1 {
		t.Fatalf("tool results = %d, want 1", got)
	}
	if startCount != 1 || finishCount != 1 {
		t.Fatalf("tool callback counts = %d/%d, want 1/1", startCount, finishCount)
	}
}

func TestStreamText_OnStepFinishEventIncludesResponseMessagesAndStepLocalSources(t *testing.T) {
	t.Parallel()

	callCount := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(_ context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{
						SourceType: "url",
						ID:         "src-1",
						URL:        "https://example.com/1",
						Title:      "One",
					}},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID: "tc1", ToolName: "echo", Arguments: map[string]interface{}{"value": "a"},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{
					SourceType: "url",
					ID:         "src-2",
					URL:        "https://example.com/2",
					Title:      "Two",
				}},
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	tool := types.Tool{
		Name:       "echo",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(_ context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}

	var stepEvents []OnStepFinishEvent
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "go",
		Tools:  []types.Tool{tool},
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			stepEvents = append(stepEvents, e)
		},
		OnFinishEvent: func(_ context.Context, _ OnFinishEvent) {},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	steps := result.Steps()
	if len(steps) != 2 || len(stepEvents) != 2 {
		t.Fatalf("steps/events = %d/%d, want 2/2", len(steps), len(stepEvents))
	}
	if len(stepEvents[0].Response.Messages) == 0 {
		t.Fatal("first step finish event response messages were empty")
	}
	if len(stepEvents[0].Sources) != 1 || stepEvents[0].Sources[0].ID != "src-1" {
		t.Fatalf("first step sources = %#v, want only src-1", stepEvents[0].Sources)
	}
	if len(stepEvents[1].Sources) != 1 || stepEvents[1].Sources[0].ID != "src-2" {
		t.Fatalf("second step sources = %#v, want only src-2", stepEvents[1].Sources)
	}
	if len(steps[0].Sources) != 1 || steps[0].Sources[0].ID != "src-1" {
		t.Fatalf("step[0] sources = %#v, want only src-1", steps[0].Sources)
	}
	if len(steps[1].Sources) != 1 || steps[1].Sources[0].ID != "src-2" {
		t.Fatalf("step[1] sources = %#v, want only src-2", steps[1].Sources)
	}
}
