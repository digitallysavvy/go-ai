package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// An abort in a later step reports the steps completed before it.
func TestGenerateText_AbortEventCarriesCompletedSteps(t *testing.T) {
	t.Parallel()
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
				}, nil
			}
			return nil, context.Canceled
		},
	}
	tool := types.Tool{
		Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	var got *GenerateTextAbortEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "go", Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(5)},
		OnAbortEvent: func(_ context.Context, e GenerateTextAbortEvent) { got = &e },
	})
	if err == nil || got == nil {
		t.Fatalf("err=%v event=%v", err, got)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("abort event steps = %d, want 1 completed step", len(got.Steps))
	}
}
