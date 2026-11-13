package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS shouldStartNextStep checks `finishReason === 'interrupted'` first, so an
// interrupted step that requested a tool approval ends the run with the
// steering reason left at 'finished', not 'waiting-for-tool-results'.
func TestR68InterruptedStepWithApprovalEndsSteeringFinished(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(int) *types.GenerateResult {
		r := toolCallResult()
		r.FinishReason = types.FinishReasonInterrupted
		return r
	}}
	tools := []types.Tool{{
		Name:         "tool1",
		ToolApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "q", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
		OnStepEnd: func(ctx context.Context, s types.StepResult, uc interface{}) { handle = steering.Steer("more") },
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawRequest bool
	for _, p := range res.Steps[0].Content {
		if _, ok := p.(types.ToolApprovalRequestContent); ok {
			sawRequest = true
		}
	}
	if !sawRequest {
		t.Fatalf("expected an approval request in the step content: %#v", res.Steps[0].Content)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringFinished {
		t.Fatalf("outcome = %+v, want not delivered with reason %q", o, SteeringFinished)
	}
}

func TestR68StreamInterruptedStepWithApprovalEndsSteeringFinished(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	var mu sync.Mutex
	var prompts [][]string
	model := steerStreamModel(func(call int) []provider.StreamChunk {
		handle = steering.Steer("more")
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonInterrupted, Usage: &types.Usage{}},
		}
	}, &prompts, &mu)
	tools := []types.Tool{{
		Name:         "tool1",
		ToolApproval: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "q", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatal(err)
	}
	var sawRequest bool
	for _, p := range result.Steps()[0].Content {
		if _, ok := p.(types.ToolApprovalRequestContent); ok {
			sawRequest = true
		}
	}
	if !sawRequest {
		t.Fatalf("expected an approval request in the step content: %#v", result.Steps()[0].Content)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringFinished {
		t.Fatalf("outcome = %+v, want not delivered with reason %q", o, SteeringFinished)
	}
}
