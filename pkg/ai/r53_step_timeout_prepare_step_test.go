package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS arms the step timer at the top of each step, before prepareStep, so a
// PrepareStep that outlasts the per-step timeout times the step out.
func TestR53StepTimeoutCoversPrepareStep(t *testing.T) {
	perStep := 30 * time.Millisecond
	slowPrepare := func(context.Context, PrepareStepOptions) PrepareStepOptions {
		time.Sleep(80 * time.Millisecond)
		return PrepareStepOptions{}
	}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	t.Run("generate", func(t *testing.T) {
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "hi", PrepareStep: slowPrepare,
			Timeout: &TimeoutConfig{PerStep: &perStep},
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the step deadline", err)
		}
	})
	t.Run("stream", func(t *testing.T) {
		res, err := StreamText(context.Background(), StreamTextOptions{
			Model: model, Prompt: "hi", PrepareStep: slowPrepare,
			Timeout: &TimeoutConfig{PerStep: &perStep},
		})
		if err == nil {
			_, err = res.ReadAll()
			if err == nil {
				err = res.Err()
			}
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the step deadline", err)
		}
	})
}

// The later steps of StreamText arm their timer before PrepareStep too.
func TestR53StreamNextStepTimeoutCoversPrepareStep(t *testing.T) {
	perStep := 30 * time.Millisecond
	calls := 0
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			calls++
			if calls == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "noop", RawArguments: `{}`}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{Name: "noop", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			if o.StepNumber == 1 {
				time.Sleep(80 * time.Millisecond)
			}
			return PrepareStepOptions{}
		},
		Timeout: &TimeoutConfig{PerStep: &perStep},
	})
	if err == nil {
		_, err = res.ReadAll()
		if err == nil {
			err = res.Err()
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the second step's deadline", err)
	}
}
