package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestGenerateText_PrepareStepCallSettingOverrides verifies that PrepareStep
// can override per-step call settings (audit row 60f97f6 / WG-STEP): an
// override applies only to that step and does not carry forward to the
// next, and an explicit zero value (Temperature: 0) is honored rather than
// treated as "no override".
func TestGenerateText_PrepareStepCallSettingOverrides(t *testing.T) {
	t.Parallel()

	var seenTemps []float64
	var seenMaxTokens []int
	callCount := 0
	tool := types.Tool{
		Name: "noop",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			callCount++
			if opts.Temperature != nil {
				seenTemps = append(seenTemps, *opts.Temperature)
			}
			if opts.MaxTokens != nil {
				seenMaxTokens = append(seenMaxTokens, *opts.MaxTokens)
			}
			if callCount == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls: []types.ToolCall{{
						ID:        "call_1",
						ToolName:  "noop",
						Arguments: map[string]interface{}{},
					}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}

	outerTemp := 0.5
	outerMaxTokens := 100
	overrideTemp := 0.0 // explicit zero, must be preserved, not treated as unset
	overrideMaxTokens := 500
	maxSteps := 3
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "hi",
		Temperature: &outerTemp,
		MaxTokens:   &outerMaxTokens,
		Tools:       []types.Tool{tool},
		MaxSteps:    &maxSteps,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 0 {
				// Override only step 1's temperature (to an explicit zero)
				// and max tokens.
				step.Temperature = &overrideTemp
				step.MaxOutputTokens = &overrideMaxTokens
			}
			return step
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
	if len(seenTemps) != 2 {
		t.Fatalf("expected temperature on both calls, got %v", seenTemps)
	}
	if seenTemps[0] != 0.0 {
		t.Fatalf("step 1 temperature = %v, want 0.0 (explicit override, including an explicit zero)", seenTemps[0])
	}
	if seenTemps[1] != outerTemp {
		t.Fatalf("step 2 temperature = %v, want the outer call's %v (override must not carry forward)", seenTemps[1], outerTemp)
	}
	if len(seenMaxTokens) != 2 || seenMaxTokens[0] != overrideMaxTokens || seenMaxTokens[1] != outerMaxTokens {
		t.Fatalf("seenMaxTokens = %v, want [%d, %d]", seenMaxTokens, overrideMaxTokens, outerMaxTokens)
	}
}

// TestStreamText_PrepareStepCallSettingOverrides is the streaming analogue
// of TestGenerateText_PrepareStepCallSettingOverrides.
func TestStreamText_PrepareStepCallSettingOverrides(t *testing.T) {
	t.Parallel()

	var seenMaxTokens []int
	callCount := 0
	tool := types.Tool{
		Name: "noop",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			callCount++
			if opts.MaxTokens != nil {
				seenMaxTokens = append(seenMaxTokens, *opts.MaxTokens)
			}
			if callCount == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID:        "call_1",
						ToolName:  "noop",
						Arguments: map[string]interface{}{},
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

	outerMaxTokens := 100
	overrideMaxTokens := 500
	maxSteps := 3
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:     model,
		Prompt:    "hi",
		MaxTokens: &outerMaxTokens,
		Tools:     []types.Tool{tool},
		MaxSteps:  &maxSteps,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber == 0 {
				step.MaxOutputTokens = &overrideMaxTokens
			}
			return step
		},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
	if len(seenMaxTokens) != 2 || seenMaxTokens[0] != overrideMaxTokens || seenMaxTokens[1] != outerMaxTokens {
		t.Fatalf("seenMaxTokens = %v, want [%d, %d] (override must not carry forward)", seenMaxTokens, overrideMaxTokens, outerMaxTokens)
	}
}
