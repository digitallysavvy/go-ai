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

// TS generateText checks `mergedAbortSignal?.throwIfAborted()` at the top of
// every step after the first, so a call aborted while a step's tools ran
// (with a tool that ignores the signal) fails before the next step starts:
// no prepareStep, step start or step error for it.
func TestGenerateTextAbortBetweenStepsStopsBeforeNextStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	modelCalls, prepareCalls, stepStarts, stepErrors := 0, 0, 0, 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelCalls++
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "work", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	tool := types.Tool{
		Name:       "work",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel() // the tool ignores the abort and returns a result
			return "done", nil
		},
	}
	_, err := GenerateText(ctx, GenerateTextOptions{
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(5)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			prepareCalls++
			return PrepareStepOptions{}
		},
		OnStepStart: func(context.Context, OnStepStartEvent) { stepStarts++ },
		OnStepError: func(context.Context, OnStepErrorEvent) { stepErrors++ },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if modelCalls != 1 || prepareCalls != 1 || stepStarts != 1 || stepErrors != 0 {
		t.Fatalf("model calls %d, prepareStep %d, step starts %d, step errors %d; want 1, 1, 1, 0", modelCalls, prepareCalls, stepStarts, stepErrors)
	}
}

// A total timeout that expires while a tool that ignores ctx runs fails the
// call before the next step with the total-timeout error, as other total
// timeout paths report it.
func TestGenerateTextTotalTimeoutBetweenStepsIsTimeoutError(t *testing.T) {
	modelCalls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			modelCalls++
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "work", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	tool := types.Tool{
		Name:       "work",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			<-ctx.Done() // wait out the total timeout, then return a result anyway
			return "done", nil
		},
	}
	total := 50 * time.Millisecond
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Prompt:   "go",
		Tools:    []types.Tool{tool},
		StopWhen: []StopCondition{StepCountIs(5)},
		Timeout:  &TimeoutConfig{Total: &total},
	})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Reason != TimeoutReasonTotal {
		t.Fatalf("err = %T %[1]v, want a total TimeoutError", err)
	}
	if modelCalls != 1 {
		t.Fatalf("model calls = %d, want 1", modelCalls)
	}
}
