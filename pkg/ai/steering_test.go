package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func userText(t types.Message) string {
	var sb strings.Builder
	for _, p := range t.Content {
		if tc, ok := p.(types.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func waitOutcome(t *testing.T, h *SteerHandle) SteeringOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	o, err := h.Wait(ctx)
	if err != nil {
		t.Fatalf("steering outcome not resolved: %v", err)
	}
	return o
}

// promptRoles records the roles and user texts sent in each model call.
type steerModel struct {
	mu      sync.Mutex
	prompts [][]string
	next    func(call int) *types.GenerateResult
	calls   int
}

func (m *steerModel) model() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			var seen []string
			for _, msg := range opts.Prompt.Messages {
				if msg.Role == types.RoleUser {
					seen = append(seen, userText(msg))
				}
			}
			m.prompts = append(m.prompts, seen)
			m.calls++
			return m.next(m.calls), nil
		},
	}
}

func toolCallResult() *types.GenerateResult {
	return &types.GenerateResult{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
	}
}

func TestSteering_BeforeStartLandsInFirstStepInOrder(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	h1 := steering.Steer("first")
	h2 := steering.Steer("second")
	sm := &steerModel{next: func(int) *types.GenerateResult {
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}
	}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{Model: sm.model(), Prompt: "hello", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sm.prompts[0], "|"); got != "hello|first|second" {
		t.Fatalf("prompt users = %q", got)
	}
	for i, h := range []*SteerHandle{h1, h2} {
		o := waitOutcome(t, h)
		if !o.Delivered || o.StepNumber != 0 {
			t.Fatalf("outcome %d = %+v", i, o)
		}
	}
	if len(result.Steps[0].ExperimentalSteeringMessages) != 2 {
		t.Fatalf("step steering messages = %+v", result.Steps[0].ExperimentalSteeringMessages)
	}
}

func TestSteering_DuringToolRunDeliveredAtNextStep(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(call int) *types.GenerateResult {
		if call == 1 {
			return toolCallResult()
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}
	}}
	tools := []types.Tool{{
		Name: "tool1",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			handle = steering.Steer("I meant Paris, Texas.")
			return "ok", nil
		},
	}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "weather", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sm.prompts[1], "|"); got != "weather|I meant Paris, Texas." {
		t.Fatalf("second prompt users = %q", got)
	}
	o := waitOutcome(t, handle)
	if !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
	if len(result.Steps[0].ExperimentalSteeringMessages) != 0 || len(result.Steps[1].ExperimentalSteeringMessages) != 1 {
		t.Fatalf("steps = %+v", result.Steps)
	}
}

func TestSteering_WaitingMessageContinuesAfterFinalAnswer(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(int) *types.GenerateResult {
		return &types.GenerateResult{Text: "answer", FinishReason: types.FinishReasonStop}
	}}
	var once sync.Once
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "q", StopWhen: []StopCondition{IsStepCount(3)}, ExperimentalSteering: steering,
		OnStepEnd: func(ctx context.Context, s types.StepResult, uc interface{}) {
			once.Do(func() { handle = steering.Steer("and Tokyo?") })
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(result.Steps))
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestSteering_StopConditionEndsRun(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(int) *types.GenerateResult {
		return &types.GenerateResult{Text: "answer", FinishReason: types.FinishReasonStop}
	}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "q", ExperimentalSteering: steering, // default stopWhen is one step
		OnStepEnd: func(ctx context.Context, s types.StepResult, uc interface{}) { handle = steering.Steer("more") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringStopCondition {
		t.Fatalf("outcome = %+v", o)
	}
	// after the run ended a steer resolves as finished right away
	if o := waitOutcome(t, steering.Steer("late")); o.Delivered || o.Reason != SteeringFinished {
		t.Fatalf("late outcome = %+v", o)
	}
}

func TestSteering_ErrorEndsRunAndSameIDReturnsFirstOutcome(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	h := steering.Steer("hello", WithSteerID("m1"))
	if again := steering.Steer("different", WithSteerID("m1")); again != h {
		t.Fatal("same id must return the first handle")
	}
	boom := errors.New("provider failed")
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, boom
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering}); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	// the message was taken into the failed step's input, so it is delivered or
	// failed with the run: either way the handle must resolve.
	o := waitOutcome(t, h)
	if o.ID != "m1" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestSteering_PrepareStepRebuildingMessagesDropsSteer(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	h := steering.Steer("steered")
	sm := &steerModel{next: func(int) *types.GenerateResult {
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}
	}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "q", ExperimentalSteering: steering,
		PrepareStep: func(ctx context.Context, o PrepareStepOptions) PrepareStepOptions {
			if len(o.ExperimentalSteeringMessages) != 1 {
				t.Errorf("steering messages = %+v", o.ExperimentalSteeringMessages)
			}
			o.Messages = o.InitialMessages
			return o
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o := waitOutcome(t, h); o.Delivered || o.Reason != SteeringRemovedByPrepareStep {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestSteering_WaitingForToolResults(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(int) *types.GenerateResult { return toolCallResult() }}
	tools := []types.Tool{{Name: "tool1"}} // no Execute: the loop waits for a result
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "q", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
		OnStepEnd: func(ctx context.Context, s types.StepResult, uc interface{}) { handle = steering.Steer("more") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringWaitingForToolResults {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestSteering_ReuseAndWrongRoleAreErrors(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	sm := &steerModel{next: func(int) *types.GenerateResult {
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}
	}}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: sm.model(), Prompt: "q", ExperimentalSteering: steering}); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: sm.model(), Prompt: "q", ExperimentalSteering: steering}); err == nil {
		t.Fatal("reusing a steering object must fail")
	}
	if _, err := NewSteering().SteerMessage(types.Message{Role: types.RoleAssistant}); err == nil {
		t.Fatal("steering messages must be user messages")
	}
}

func steerStreamModel(next func(call int) []provider.StreamChunk, prompts *[][]string, mu *sync.Mutex) *testutil.MockLanguageModel {
	calls := 0
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			var seen []string
			for _, msg := range opts.Prompt.Messages {
				if msg.Role == types.RoleUser {
					seen = append(seen, userText(msg))
				}
			}
			*prompts = append(*prompts, seen)
			calls++
			return testutil.NewMockTextStream(next(calls)), nil
		},
	}
}

func TestStreamText_SteeringDeliveredAfterToolRun(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var handle *SteerHandle
	var mu sync.Mutex
	var prompts [][]string
	model := steerStreamModel(func(call int) []provider.StreamChunk {
		if call == 1 {
			return []provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{}},
			}
		}
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}
	}, &prompts, &mu)
	tools := []types.Tool{{
		Name: "tool1",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			handle = steering.Steer("I meant Paris, Texas.")
			return "ok", nil
		},
	}}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "weather", Tools: tools, StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(prompts[1], "|"); got != "weather|I meant Paris, Texas." {
		t.Fatalf("second prompt users = %q", got)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
	steps := result.Steps()
	if len(steps) != 2 || len(steps[1].ExperimentalSteeringMessages) != 1 || len(steps[0].ExperimentalSteeringMessages) != 0 {
		t.Fatalf("steps = %+v", steps)
	}
}

func TestStreamText_SteeringWhileAnswerContinuesLoopAndStopConditionWins(t *testing.T) {
	t.Parallel()

	answer := func(int) []provider.StreamChunk {
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "answer"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}},
		}
	}
	run := func(stop []StopCondition) (*StreamTextResult, SteeringOutcome) {
		steering := NewSteering()
		var handle *SteerHandle
		var once sync.Once
		var mu sync.Mutex
		var prompts [][]string
		result, err := StreamText(context.Background(), StreamTextOptions{
			Model: steerStreamModel(answer, &prompts, &mu), Prompt: "q", StopWhen: stop, ExperimentalSteering: steering,
			OnStepEnd: func(ctx context.Context, s types.StepResult, uc interface{}) {
				once.Do(func() { handle = steering.Steer("and Tokyo?") })
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := result.ReadAll(); err != nil {
			t.Fatal(err)
		}
		return result, waitOutcome(t, handle)
	}

	result, o := run([]StopCondition{IsStepCount(3)})
	if len(result.Steps()) != 2 || !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("steps=%d outcome=%+v", len(result.Steps()), o)
	}
	result, o = run(nil)
	if len(result.Steps()) != 1 || o.Delivered || o.Reason != SteeringStopCondition {
		t.Fatalf("steps=%d outcome=%+v", len(result.Steps()), o)
	}
}

func TestStreamText_SteeringResolvesWhenStreamFails(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	h := steering.Steer("hello")
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("boom")
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	o := waitOutcome(t, h)
	if o.Delivered && o.StepNumber != 0 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestStreamText_SteeringReuseIsAnError(t *testing.T) {
	t.Parallel()

	steering := NewSteering()
	var mu sync.Mutex
	var prompts [][]string
	model := steerStreamModel(func(int) []provider.StreamChunk {
		return []provider.StreamChunk{{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{}}}
	}, &prompts, &mu)
	if _, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering}); err != nil {
		t.Fatal(err)
	}
	if _, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering}); err == nil {
		t.Fatal("reusing a steering object must fail")
	}
}
