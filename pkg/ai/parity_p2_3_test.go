package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateTextAggregatesContentAcrossSteps(t *testing.T) {
	step := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if step == 0 {
				step++
				return &types.GenerateResult{
					Text:         "calling tool",
					FinishReason: types.FinishReasonToolCalls,
					Content: []types.ContentPart{
						types.TextContent{Text: "calling tool"},
						types.ToolCallContent{ToolCallID: "tool-1", ToolName: "calc", Arguments: map[string]interface{}{"x": 1}},
					},
					ToolCalls: []types.ToolCall{{
						ID:        "tool-1",
						ToolName:  "calc",
						Arguments: map[string]interface{}{"x": 1},
					}},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
				Content:      []types.ContentPart{types.TextContent{Text: "done"}},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{
			Name: "calc",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got, want := len(result.Content), 4; got != want {
		t.Fatalf("len(result.Content) = %d, want %d", got, want)
	}
	if txt, ok := result.Content[0].(types.TextContent); !ok || txt.Text != "calling tool" {
		t.Fatalf("content[0] = %#v, want first-step text", result.Content[0])
	}
	if tc, ok := result.Content[1].(types.ToolCallContent); !ok || tc.ToolCallID != "tool-1" {
		t.Fatalf("content[1] = %#v, want tool call tool-1", result.Content[1])
	}
	if tr, ok := result.Content[2].(types.ToolResultContent); !ok || tr.ToolCallID != "tool-1" {
		t.Fatalf("content[2] = %#v, want local tool result for tool-1", result.Content[2])
	}
	if txt, ok := result.Content[3].(types.TextContent); !ok || txt.Text != "done" {
		t.Fatalf("content[3] = %#v, want second-step text", result.Content[3])
	}
}

func TestStreamTextContentAggregatesAcrossSteps(t *testing.T) {
	step := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			if step == 0 {
				step++
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "calling tool"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "tool-1",
						ToolName:  "calc",
						Arguments: map[string]interface{}{"x": 1},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{}},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
			}), nil
		},
	}

	done := make(chan *StreamTextResult, 1)
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{
			Name: "calc",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(2)},
		OnFinish: func(r *StreamTextResult) { done <- r },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case result = <-done:
		if err := result.Err(); err != nil {
			t.Fatalf("stream err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}
	content := result.Content()
	if got, want := len(content), 4; got != want {
		t.Fatalf("len(result.Content()) = %d, want %d: %#v", got, want, content)
	}
	if txt, ok := content[0].(types.TextContent); !ok || txt.Text != "calling tool" {
		t.Fatalf("content[0] = %#v, want first-step text", content[0])
	}
	if tc, ok := content[1].(types.ToolCallContent); !ok || tc.ToolCallID != "tool-1" {
		t.Fatalf("content[1] = %#v, want tool call tool-1", content[1])
	}
	if tr, ok := content[2].(types.ToolResultContent); !ok || tr.ToolCallID != "tool-1" {
		t.Fatalf("content[2] = %#v, want local tool result for tool-1", content[2])
	}
	if txt, ok := content[3].(types.TextContent); !ok || txt.Text != "done" {
		t.Fatalf("content[3] = %#v, want second-step text", content[3])
	}
}

func TestGenerateTextAggregatesFilesSourcesWarningsAcrossSteps(t *testing.T) {
	step := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if step == 0 {
				step++
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					Content: []types.ContentPart{
						types.SourceContent{ID: "src-1", Title: "first"},
						types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("first")},
						types.ToolCallContent{ToolCallID: "tool-1", ToolName: "calc", Arguments: map[string]interface{}{}},
					},
					ToolCalls: []types.ToolCall{{
						ID:        "tool-1",
						ToolName:  "calc",
						Arguments: map[string]interface{}{},
					}},
					Warnings: []types.Warning{{Type: "other", Message: "step 0 warning"}},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
				Content: []types.ContentPart{
					types.SourceContent{ID: "src-2", Title: "second"},
					types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("second")},
					types.TextContent{Text: "done"},
				},
				Warnings: []types.Warning{{Type: "other", Message: "step 1 warning"}},
			}, nil
		},
	}

	var finishEvent OnFinishEvent
	var finishResult *GenerateTextResult
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{
			Name: "calc",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			},
		}},
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) {
			finishEvent = e
		},
		OnFinish: func(_ context.Context, r *GenerateTextResult, _ interface{}) {
			finishResult = r
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got, want := len(result.Sources), 2; got != want {
		t.Fatalf("len(result.Sources) = %d, want %d", got, want)
	}
	if result.Sources[0].ID != "src-1" || result.Sources[1].ID != "src-2" {
		t.Fatalf("sources = %+v, want all step sources in order", result.Sources)
	}
	if got, want := len(result.Files), 2; got != want {
		t.Fatalf("len(result.Files) = %d, want %d", got, want)
	}
	if string(result.Files[0].Data) != "first" || string(result.Files[1].Data) != "second" {
		t.Fatalf("files = %+v, want all step files in order", result.Files)
	}
	if got, want := len(result.Warnings), 2; got != want {
		t.Fatalf("len(result.Warnings) = %d, want %d", got, want)
	}
	if result.Warnings[0].Message != "step 0 warning" || result.Warnings[1].Message != "step 1 warning" {
		t.Fatalf("warnings = %+v, want all step warnings in order", result.Warnings)
	}
	if got, want := len(result.FinalStep.Warnings), 1; got != want {
		t.Fatalf("len(result.FinalStep.Warnings) = %d, want %d", got, want)
	}
	if got, want := len(finishEvent.Sources), 1; got != want {
		t.Fatalf("len(finishEvent.Sources) = %d, want final-step sources only", got)
	}
	if finishEvent.Sources[0].ID != "src-2" {
		t.Fatalf("finishEvent.Sources = %+v, want final-step source", finishEvent.Sources)
	}
	if got, want := len(finishEvent.Files), 2; got != want {
		t.Fatalf("len(finishEvent.Files) = %d, want all-step files", got)
	}
	if got, want := len(finishEvent.Warnings), 2; got != want {
		t.Fatalf("len(finishEvent.Warnings) = %d, want all-step warnings", got)
	}
	if got := len(result.ToolCalls); got != 1 {
		t.Fatalf("len(result.ToolCalls) = %d, want all-step tool calls", got)
	}
	if got := len(finishEvent.ToolCalls); got != 0 {
		t.Fatalf("len(finishEvent.ToolCalls) = %d, want final-step tool calls only", got)
	}
	if got := len(result.ToolResults); got != 1 {
		t.Fatalf("len(result.ToolResults) = %d, want all-step tool results", got)
	}
	if got := len(finishEvent.ToolResults); got != 0 {
		t.Fatalf("len(finishEvent.ToolResults) = %d, want final-step tool results only", got)
	}
	if finishResult == nil {
		t.Fatal("OnFinish did not receive result")
	}
	if finishResult.TotalUsage.InputTokens != finishResult.Usage.InputTokens ||
		finishResult.TotalUsage.OutputTokens != finishResult.Usage.OutputTokens ||
		finishResult.TotalUsage.TotalTokens != finishResult.Usage.TotalTokens {
		t.Fatalf("OnFinish TotalUsage = %+v, want %+v", finishResult.TotalUsage, finishResult.Usage)
	}
	if finishResult.FinalStep.StepNumber != result.FinalStep.StepNumber {
		t.Fatalf("OnFinish FinalStep = %+v, want %+v", finishResult.FinalStep, result.FinalStep)
	}
}

func TestGenerateTextSandboxDescriptionAppendedToSystem(t *testing.T) {
	var seenSystem string
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			seenSystem = opts.Prompt.System
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:               model,
		Prompt:              "hello",
		System:              "You are helpful.",
		ExperimentalSandbox: NewShellSandbox(WithShellSandboxDescription("Ubuntu 22.04, root: /workspace")),
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if seenSystem == "" || !containsSubstring(seenSystem, "Ubuntu 22.04, root: /workspace") {
		t.Fatalf("system prompt missing sandbox description: %q", seenSystem)
	}
}

func TestGenerateTextStepTimeMsIsPopulatedForToolStep(t *testing.T) {
	outputTokens := int64(1)
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls: []types.ToolCall{{
					ID:        "call-1",
					ToolName:  "lookup",
					Arguments: map[string]interface{}{},
				}},
				Usage: types.Usage{OutputTokens: &outputTokens},
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hello",
		Tools: []types.Tool{{
			Name: "lookup",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				time.Sleep(time.Millisecond)
				return "ok", nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got := result.FinalStep.Performance.StepTimeMs; got <= 0 {
		t.Fatalf("stepTimeMs = %d, want > 0", got)
	}
}

func TestStreamTextSandboxDescriptionAppliedOnEveryStep(t *testing.T) {
	call := 0
	var seenSystems []string
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			seenSystems = append(seenSystems, opts.Prompt.System)
			if call == 0 {
				call++
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "calc", Arguments: map[string]interface{}{"x": 1}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{}},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
			}), nil
		},
	}

	done := make(chan *StreamTextResult, 1)
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model:               model,
		Prompt:              "hi",
		System:              "You are helpful.",
		ExperimentalSandbox: NewShellSandbox(WithShellSandboxDescription("Ubuntu 22.04, root: /workspace")),
		StopWhen:            []StopCondition{StepCountIs(2)},
		Tools: []types.Tool{{
			Name: "calc",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			},
		}},
		OnFinish: func(r *StreamTextResult) { done <- r },
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	select {
	case r := <-done:
		if err := r.Err(); err != nil {
			t.Fatalf("stream err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}
	if got := len(seenSystems); got != 2 {
		t.Fatalf("stream calls = %d, want 2", got)
	}
	for i, system := range seenSystems {
		if !containsSubstring(system, "Ubuntu 22.04, root: /workspace") {
			t.Fatalf("call %d system missing sandbox description: %q", i+1, system)
		}
	}
}

func containsSubstring(value, needle string) bool {
	return len(needle) > 0 && len(value) > 0 && (len(value) >= len(needle)) && (indexOf(value, needle) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
