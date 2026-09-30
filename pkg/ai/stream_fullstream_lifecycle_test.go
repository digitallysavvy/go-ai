package ai

import (
	"context"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestStreamText_FullStreamLifecycleSingleStep ports TS's
// stream-text.test.ts describe('options.onChunk') > it('should return events
// in order') fullStream ordering: 'start' once at the very beginning,
// 'start-step' before the step's own content, and 'finish-step' followed by
// the call-level 'finish' at the end (stream-text.ts:1855, 2829-2838,
// 3020-3033, 3129-3136). Checked via Stream() (the fullStream analogue), not
// just OnChunk, per gap A1-1.
func TestStreamText_FullStreamLifecycleSingleStep(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: int64Ptr(3), OutputTokens: int64Ptr(10), TotalTokens: int64Ptr(13)}},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input"})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	var got []provider.ChunkType
	stream := result.Stream()
	var startStep, finishStep *provider.StreamChunk
	var finalFinish *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		got = append(got, chunk.Type)
		switch chunk.Type {
		case provider.ChunkTypeStartStep:
			c := *chunk
			startStep = &c
		case provider.ChunkTypeFinishStep:
			c := *chunk
			finishStep = &c
		case provider.ChunkTypeFinish:
			c := *chunk
			finalFinish = &c
		}
	}

	want := []provider.ChunkType{
		provider.ChunkTypeStart,
		provider.ChunkTypeStartStep,
		provider.ChunkTypeFirstChunk,
		provider.ChunkTypeText,
		provider.ChunkTypeFinishStep,
		provider.ChunkTypeFinish,
		provider.ChunkTypeStreamFinish,
	}
	if len(got) != len(want) {
		t.Fatalf("chunk types = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk types = %#v, want %#v", got, want)
		}
	}

	if startStep == nil || startStep.Request == nil {
		t.Fatalf("start-step chunk missing Request: %#v", startStep)
	}
	if startStep.Warnings == nil {
		t.Fatalf("start-step chunk should carry a non-nil (possibly empty) Warnings slice: %#v", startStep)
	}

	if finishStep == nil {
		t.Fatal("missing finish-step chunk")
	}
	if finishStep.FinishReason != types.FinishReasonStop {
		t.Errorf("finish-step FinishReason = %q, want stop", finishStep.FinishReason)
	}
	if finishStep.Usage == nil || finishStep.Usage.TotalTokens == nil || *finishStep.Usage.TotalTokens != 13 {
		t.Errorf("finish-step Usage = %#v, want TotalTokens=13", finishStep.Usage)
	}
	if finishStep.Response == nil {
		t.Error("finish-step Response is nil")
	}
	if finishStep.Performance == nil {
		t.Error("finish-step Performance is nil")
	}

	if finalFinish == nil {
		t.Fatal("missing call-level finish chunk")
	}
	if finalFinish.FinishReason != types.FinishReasonStop {
		t.Errorf("finish FinishReason = %q, want stop", finalFinish.FinishReason)
	}
	if finalFinish.Usage == nil || finalFinish.Usage.TotalTokens == nil || *finalFinish.Usage.TotalTokens != 13 {
		t.Errorf("finish Usage = %#v, want TotalTokens=13 (totalUsage)", finalFinish.Usage)
	}
}

// TestStreamText_FullStreamLifecycleMultiStep ports the shape of TS's
// stream-text.test.ts describe('2 steps: initial, tool-result') fullStream
// ordering to a tool-calling multi-step run: exactly
// start, start-step, ..., finish-step, start-step, ..., finish-step, finish
// with one start-step/finish-step pair per step and exactly one call-level
// start/finish (gap A1-1's own acceptance test).
func TestStreamText_FullStreamLifecycleMultiStep(t *testing.T) {
	t.Parallel()

	step := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			step++
			if step == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: int64Ptr(1), OutputTokens: int64Ptr(2), TotalTokens: int64Ptr(3)}},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: int64Ptr(4), OutputTokens: int64Ptr(5), TotalTokens: int64Ptr(9)}},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test-input",
		Tools: []types.Tool{{
			Name: "tool1",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "result1", nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	var got []provider.ChunkType
	var finishSteps []provider.StreamChunk
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error = %v", err)
		}
		got = append(got, chunk.Type)
		if chunk.Type == provider.ChunkTypeFinishStep {
			finishSteps = append(finishSteps, *chunk)
		}
	}

	want := []provider.ChunkType{
		provider.ChunkTypeStart,
		provider.ChunkTypeStartStep,
		provider.ChunkTypeFirstChunk,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeToolResult,
		provider.ChunkTypeFinishStep,
		provider.ChunkTypeStartStep,
		provider.ChunkTypeText,
		provider.ChunkTypeFinishStep,
		provider.ChunkTypeFinish,
		provider.ChunkTypeStreamFinish,
	}
	if len(got) != len(want) {
		t.Fatalf("chunk types = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk types = %#v, want %#v", got, want)
		}
	}

	if len(finishSteps) != 2 {
		t.Fatalf("expected 2 finish-step chunks, got %d: %#v", len(finishSteps), finishSteps)
	}
	if finishSteps[0].FinishReason != types.FinishReasonToolCalls {
		t.Errorf("step 1 finish-step FinishReason = %q, want tool-calls", finishSteps[0].FinishReason)
	}
	if finishSteps[1].FinishReason != types.FinishReasonStop {
		t.Errorf("step 2 finish-step FinishReason = %q, want stop", finishSteps[1].FinishReason)
	}
	// Each finish-step's Usage is scoped to that step, not the running total.
	if finishSteps[0].Usage == nil || finishSteps[0].Usage.TotalTokens == nil || *finishSteps[0].Usage.TotalTokens != 3 {
		t.Errorf("step 1 finish-step Usage = %#v, want TotalTokens=3", finishSteps[0].Usage)
	}
	if finishSteps[1].Usage == nil || finishSteps[1].Usage.TotalTokens == nil || *finishSteps[1].Usage.TotalTokens != 9 {
		t.Errorf("step 2 finish-step Usage = %#v, want TotalTokens=9", finishSteps[1].Usage)
	}
}
