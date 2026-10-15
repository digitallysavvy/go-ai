package ai

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS continues the loop after a call to an unknown tool (it is answered with
// a tool-error) so the model can recover; it does not end the run.
func TestUnknownToolCallContinuesLoop(t *testing.T) {
	tools := []types.Tool{{Name: "real", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}}
	call := types.ToolCall{ID: "1", ToolName: "nope", Arguments: map[string]interface{}{}}

	t.Run("generate", func(t *testing.T) {
		var n int32
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			if atomic.AddInt32(&n, 1) == 1 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{call}}, nil
			}
			return &types.GenerateResult{Text: "recovered", FinishReason: types.FinishReasonStop}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Steps) != 2 || res.Text != "recovered" {
			t.Fatalf("steps=%d text=%q, want 2 steps and recovery", len(res.Steps), res.Text)
		}
	})

	t.Run("stream", func(t *testing.T) {
		var n int32
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			if atomic.AddInt32(&n, 1) == 1 {
				c := call
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &c},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "recovered"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := res.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if len(res.Steps()) != 2 || res.Text() != "recovered" {
			t.Fatalf("steps=%d text=%q, want 2 steps and recovery", len(res.Steps()), res.Text())
		}
	})
}
