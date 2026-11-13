package ai

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-text: an error part sets stepFinishReason = 'error', whatever the
// step's stream reported before it and whatever an earlier step finished with.
func TestStreamText_ErrorPartFinishesTheStepWithError(t *testing.T) {
	t.Parallel()
	t.Run("after a finish part", func(t *testing.T) {
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "hi"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
					{Type: provider.ChunkTypeError, Text: "boom"},
				}), nil
			},
		}
		result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
		if err != nil {
			t.Fatalf("StreamText() error = %v", err)
		}
		_, _ = result.ReadAll()
		if got := result.Steps()[0].FinishReason; got != types.FinishReasonError {
			t.Errorf("step finish reason = %q, want error", got)
		}
	})
	t.Run("in a later step", func(t *testing.T) {
		var calls atomic.Int32
		model := &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				if calls.Add(1) == 1 {
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				}
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "partial"},
					{Type: provider.ChunkTypeError, Text: "boom"},
				}), nil
			},
		}
		tool := types.Tool{
			Name:       "t",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}
		result, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Prompt: "hi", Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(3)},
		})
		if err != nil {
			t.Fatalf("StreamText() error = %v", err)
		}
		_, _ = result.ReadAll()
		steps := result.Steps()
		if len(steps) != 2 {
			t.Fatalf("steps = %d, want 2", len(steps))
		}
		if got := steps[1].FinishReason; got != types.FinishReasonError {
			t.Errorf("second step finish reason = %q, want error", got)
		}
	})
}
