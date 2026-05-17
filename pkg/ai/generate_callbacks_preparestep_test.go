package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateTextPrepareStepModelOverrideUpdatesFinishCallbacks(t *testing.T) {
	t.Parallel()

	firstModel := &testutil.MockLanguageModel{
		ProviderName: "mock",
		ModelName:    "first",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc", ToolName: "echo", Arguments: map[string]interface{}{"value": "x"}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	secondModel := &testutil.MockLanguageModel{
		ProviderName: "mock",
		ModelName:    "second",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
			}, nil
		},
	}

	callCount := 0
	firstModel.DoGenerateFunc = func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		callCount++
		if callCount == 1 {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "tc", ToolName: "echo", Arguments: map[string]interface{}{"value": "x"}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		}
		return secondModel.DoGenerate(context.Background(), nil)
	}

	var stepModels []string
	var finalModel string
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: firstModel,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "start"}},
		}},
		Tools: []types.Tool{{
			Name: "echo",
			Execute: func(_ context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				return input["value"], nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(2)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 1 {
				step.Model = secondModel
			}
			return step
		},
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			stepModels = append(stepModels, e.ModelID)
		},
		OnFinishEvent: func(_ context.Context, e OnFinishEvent) {
			finalModel = e.ModelID
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if len(stepModels) != 2 || stepModels[0] != "first" || stepModels[1] != "second" {
		t.Fatalf("stepModels = %#v, want first then second", stepModels)
	}
	if finalModel != "second" {
		t.Fatalf("finalModel = %q, want second", finalModel)
	}
}
