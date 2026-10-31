package ai

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r71genToolStepModel answers the first call with a tool1 call and the
// later ones with "Done.", each response from next(n) when it is set.
func r71genToolStepModel(next func(n int, r *types.GenerateResult) (*types.GenerateResult, error)) *testutil.MockLanguageModel {
	n := 0
	return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		defer func() { n++ }()
		r := &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "Done."}}, FinishReason: types.FinishReasonStop}
		if n == 0 {
			r = &types.GenerateResult{
				Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Input: `{ "value": "value" }`}},
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
				FinishReason: types.FinishReasonToolCalls,
			}
		}
		if next != nil {
			return next(n, r)
		}
		return r, nil
	}}
}

// 'result.warnings' > 'should send warnings from all steps to onFinish': the
// end event's warnings span every step; the last step keeps its own.
func TestR71Gen_OnEndWarningsFromAllSteps(t *testing.T) {
	w0 := types.Warning{Type: "other", Message: "step 0 warning"}
	w1 := types.Warning{Type: "other", Message: "step 1 warning"}
	var end OnFinishEvent
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71genToolStepModel(func(n int, r *types.GenerateResult) (*types.GenerateResult, error) {
			r.Warnings = []types.Warning{[]types.Warning{w0, w1}[n]}
			return r, nil
		}),
		Tools:      r71genTool1(func() (interface{}, error) { return "result1", nil }),
		Prompt:     "prompt",
		StopWhen:   []StopCondition{IsStepCount(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(end.Warnings, []types.Warning{w0, w1}) {
		t.Errorf("onEnd warnings = %+v, want both steps' warnings", end.Warnings)
	}
	if n := len(end.Steps); n != 2 || !reflect.DeepEqual(end.Steps[n-1].Warnings, []types.Warning{w1}) {
		t.Errorf("last step warnings = %+v", end.Steps)
	}
}

// 'options.onStepError' > 'should report the step that failed in a
// multi-step run': the first step ends, the second fails; OnStepError is
// called once, for step 1, with the provider error, which GenerateText
// returns as is.
func TestR71Gen_OnStepErrorMultiStep(t *testing.T) {
	providerErr := errors.New("provider failure")
	var stepEnds, stepErrors []int
	var stepErr error
	zero := 0
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71genToolStepModel(func(n int, r *types.GenerateResult) (*types.GenerateResult, error) {
			if n > 0 {
				return nil, providerErr
			}
			return r, nil
		}),
		Tools:          r71genTool1(func() (interface{}, error) { return "result1", nil }),
		Prompt:         "test-input",
		MaxRetries:     &zero,
		StopWhen:       []StopCondition{IsStepCount(3)},
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { stepEnds = append(stepEnds, e.StepNumber) },
		OnStepError: func(_ context.Context, e OnStepErrorEvent) {
			stepErrors = append(stepErrors, e.StepNumber)
			stepErr = e.Error
		},
	})
	if err != providerErr {
		t.Fatalf("err = %v, want the provider error itself", err)
	}
	if !reflect.DeepEqual(stepEnds, []int{0}) || !reflect.DeepEqual(stepErrors, []int{1}) || stepErr != providerErr {
		t.Errorf("onStepEnd steps = %v, onStepError steps = %v (error %v)", stepEnds, stepErrors, stepErr)
	}
}

// 'options.stopWhen' > '2 stop conditions': every stop condition is called
// (in order) with the steps so far, and the run stops after one step.
func TestR71Gen_TwoStopConditions(t *testing.T) {
	type stopCall struct {
		number int
		steps  int
		callID string
	}
	var calls []stopCall
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    r71genToolStepModel(nil),
		Tools:    r71genTool1(func() (interface{}, error) { return "result1", nil }),
		Prompt:   "test-input",
		Internal: &InternalOptions{GenerateCallID: func() string { return "test-telemetry-call-id" }},
		StopWhen: []StopCondition{
			func(s StopConditionState) string {
				calls = append(calls, stopCall{0, len(s.Steps), s.Steps[0].CallID})
				return ""
			},
			func(s StopConditionState) string {
				calls = append(calls, stopCall{1, len(s.Steps), s.Steps[0].CallID})
				return "stop"
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Errorf("steps = %d, want 1", len(res.Steps))
	}
	want := []stopCall{{0, 1, "test-telemetry-call-id"}, {1, 1, "test-telemetry-call-id"}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("stop condition calls = %+v, want %+v", calls, want)
	}
}

// 'onFinishResult should expose deprecated AI SDK 6 final-step properties':
// the end event's reasoning, reasoningText, request, response and
// providerMetadata are the final step's.
func TestR71Gen_OnEndDeprecatedFinalStepProperties(t *testing.T) {
	var end OnFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r71genToolStepModel(func(n int, r *types.GenerateResult) (*types.GenerateResult, error) {
			r.ProviderMetadata = map[string]interface{}{"test": map[string]interface{}{"step": float64(n)}}
			r.Content = append([]types.ContentPart{types.ReasoningContent{Text: "thinking " + string(rune('0'+n))}}, r.Content...)
			r.RawRequest = map[string]interface{}{"step": n}
			r.ResponseMetadata = &types.ResponseMetadata{ID: "id-" + string(rune('0'+n)), ModelID: "m"}
			return r, nil
		}),
		Tools:      r71genTool1(func() (interface{}, error) { return "result1", nil }),
		Prompt:     "test-input",
		StopWhen:   []StopCondition{IsStepCount(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	fs := res.FinalStep
	if fs.StepNumber != 1 || fs.ReasoningText != "thinking 1" {
		t.Fatalf("final step = %+v", fs)
	}
	if !reflect.DeepEqual(end.Reasoning, fs.Reasoning) || end.ReasoningText != fs.ReasoningText {
		t.Errorf("onEnd reasoning = %+v / %q, want the final step's %+v / %q", end.Reasoning, end.ReasoningText, fs.Reasoning, fs.ReasoningText)
	}
	if !reflect.DeepEqual(end.Request.Body, fs.Request.Body) {
		t.Errorf("onEnd request body = %v, want %v", end.Request.Body, fs.Request.Body)
	}
	if end.Response.ID != fs.Response.ID || end.Response.ModelID != fs.Response.ModelID || !end.Response.Timestamp.Equal(fs.Response.Timestamp) {
		t.Errorf("onEnd response = %+v, want the final step's %+v", end.Response, fs.Response)
	}
	if !reflect.DeepEqual(end.ProviderMetadata, fs.ProviderMetadata) {
		t.Errorf("onEnd providerMetadata = %v, want %v", end.ProviderMetadata, fs.ProviderMetadata)
	}
	if !reflect.DeepEqual(end.FinalStep, end.Steps[len(end.Steps)-1]) {
		t.Errorf("onEnd finalStep is not the last step")
	}
}
