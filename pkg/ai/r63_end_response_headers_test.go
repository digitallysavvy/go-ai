package ai

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText's onEnd event reports the final step's response
// (lastStep.response, headers included), also when the run ends on a step
// with tool calls (a stop condition or an approval pause).
func TestR63GenerateTextEndEventHasTheFinalStepResponseHeaders(t *testing.T) {
	step := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		step++
		return &types.GenerateResult{
			ResponseHeaders: map[string]string{"x-step": fmt.Sprint(step)},
			ToolCalls:       []types.ToolCall{{ID: fmt.Sprintf("c%d", step), ToolName: "a", Arguments: map[string]interface{}{}}},
			FinishReason:    types.FinishReasonToolCalls,
		}, nil
	}}
	var ev OnFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "go",
		Tools: []types.Tool{{Name: "a", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
		StopWhen:   []StopCondition{StepCountIs(2)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { ev = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(res.Steps))
	}
	want := map[string]string{"x-step": "2"}
	if fmt.Sprint(ev.Response.Headers) != fmt.Sprint(want) {
		t.Errorf("end event Response.Headers = %v, want %v", ev.Response.Headers, want)
	}
	if fmt.Sprint(ev.ResponseHeaders) != fmt.Sprint(want) {
		t.Errorf("end event ResponseHeaders = %v, want %v", ev.ResponseHeaders, want)
	}
	if fmt.Sprint(res.ResponseHeaders) != fmt.Sprint(want) { //nolint:staticcheck // deprecated field still filled
		t.Errorf("result ResponseHeaders = %v, want %v", res.ResponseHeaders, want) //nolint:staticcheck // deprecated field still filled
	}
}
