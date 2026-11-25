package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Steering interrupt twins (TS e4039889ba, generate-text-steering-interrupt.test.ts
// "should redo a later step with the tool results of the step before it",
// "should not deliver an interrupt message that prepareStep removes on the
// redo", "should keep the step timeout running across the redo").

func port2genRoles(opts *provider.GenerateOptions) []types.MessageRole {
	var out []types.MessageRole
	for _, m := range opts.Prompt.Messages {
		out = append(out, m.Role)
	}
	return out
}

func TestPort2genSteeringInterruptRedoesALaterStepWithTheToolResultsOfTheStepBeforeIt(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	executed := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			calls = append(calls, opts)
			n := len(calls)
			mu.Unlock()
			switch n {
			case 1:
				return toolCallResult(), nil
			case 2:
				handle = steering.Steer("Answer in Celsius.", WithSteerInterrupt())
				<-ctx.Done()
				return nil, context.Cause(ctx)
			}
			return portgenText("It is 22 degrees in Paris.", 10), nil
		}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(3)},
		ExperimentalSteering: steering,
		Tools: []types.Tool{{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed++
			return map[string]interface{}{"temperature": 72.0}, nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || executed != 1 || len(result.Steps) != 2 {
		t.Fatalf("calls = %d, executed = %d, steps = %d", len(calls), executed, len(result.Steps))
	}
	if got, want := port2genRoles(calls[2]), []types.MessageRole{types.RoleUser, types.RoleAssistant, types.RoleTool, types.RoleUser}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	if got := portgenUserTexts(calls[2]); got[len(got)-1] != "Answer in Celsius." {
		t.Fatalf("users = %v", got)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPort2genSteeringInterruptMessageRemovedByPrepareStepIsNotDeliveredOnTheRedo(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	h := &portgenHangingModel{}
	model := h.model(func() { handle = steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt()) },
		func(*provider.GenerateOptions) *types.GenerateResult { return portgenText("It is sunny in Paris.", 10) })
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			var kept []types.Message
			for _, m := range step.Messages {
				if m.Role == types.RoleUser && userText(m) == "Use Paris, Texas instead." {
					continue
				}
				kept = append(kept, m)
			}
			step.Messages = kept
			return step
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 2 {
		t.Fatalf("calls = %d", len(h.calls))
	}
	if got := portgenUserTexts(h.calls[1]); !reflect.DeepEqual(got, []string{"What is the weather in Paris?"}) {
		t.Fatalf("second call users = %v", got)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringRemovedByPrepareStep {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPort2genSteeringInterruptKeepsTheStepTimeoutRunningAcrossTheRedo(t *testing.T) {
	steering := NewSteering()
	perStep := 400 * time.Millisecond
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			<-ctx.Done()
			return nil, context.Cause(ctx)
		}}
	started := time.Now()
	go func() {
		time.Sleep(240 * time.Millisecond)
		steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt())
	}()
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		Timeout: &TimeoutConfig{PerStep: &perStep}, MaxRetries: intPtr(0),
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected the step timeout")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("calls = %d, want the redo to run once", calls)
	}
	// A timeout that started again with the redo would end at about 640ms.
	if elapsed < 380*time.Millisecond || elapsed > 560*time.Millisecond {
		t.Fatalf("elapsed = %v, want about the original %v", elapsed, perStep)
	}
}
