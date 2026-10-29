package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS ToolLoopAgent passes onStepError to generateText: a step that started
// and then fails (here the model call) ends with onStepError, carrying the
// step-start event and the error. The legacy loop never called it.
func TestR65GenerateAgentFailedStepCallsOnStepError(t *testing.T) {
	boom := errors.New("boom")
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return nil, boom
	}}
	var mu sync.Mutex
	var starts []ai.OnStepStartEvent
	var stepErrors []ai.OnStepErrorEvent
	zero := 0
	_, err := NewToolLoopAgent(AgentConfig{
		Model:      model,
		MaxRetries: &zero,
		OnStepStartEvent: func(_ context.Context, e ai.OnStepStartEvent) {
			mu.Lock()
			starts = append(starts, e)
			mu.Unlock()
		},
		OnStepError: func(_ context.Context, e ai.OnStepErrorEvent) {
			mu.Lock()
			stepErrors = append(stepErrors, e)
			mu.Unlock()
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 1 || len(stepErrors) != 1 {
		t.Fatalf("step starts %d, step errors %d; want 1 and 1", len(starts), len(stepErrors))
	}
	if !errors.Is(stepErrors[0].Error, boom) || stepErrors[0].CallID == "" || stepErrors[0].CallID != starts[0].CallID || stepErrors[0].StepNumber != 0 {
		t.Fatalf("step error event = %+v, want boom with the step-start event", stepErrors[0])
	}
}

// Invalid instruction messages fail the legacy loop with TS standardizePrompt's
// InvalidPromptError. The step had started (this loop reports the step start
// before the prompt is converted, as TS WorkflowAgent), so it ends with
// OnStepError.
func TestR65GenerateAgentInvalidInstructionsEndStartedStep(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		t.Error("model called")
		return &types.GenerateResult{FinishReason: types.FinishReasonStop}, nil
	}}
	var mu sync.Mutex
	starts, stepErrors := 0, 0
	_, err := NewToolLoopAgent(AgentConfig{
		Model:               model,
		InstructionMessages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}},
		OnStepStartEvent: func(context.Context, ai.OnStepStartEvent) {
			mu.Lock()
			starts++
			mu.Unlock()
		},
		OnStepError: func(context.Context, ai.OnStepErrorEvent) {
			mu.Lock()
			stepErrors++
			mu.Unlock()
		},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	var promptErr *providererrors.InvalidPromptError
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &promptErr) || !errors.As(err, &argErr) {
		t.Fatalf("err = %v, want an InvalidPromptError (with the v0.5 InvalidArgumentError cause)", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 || stepErrors != 1 {
		t.Fatalf("step starts %d, step errors %d; want 1 and 1", starts, stepErrors)
	}
}

type r65StepEndRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	steps []telemetry.TelemetryStepEndEvent
}

func (r *r65StepEndRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	r.steps = append(r.steps, e)
	r.mu.Unlock()
}

// GenerateText's telemetry step end carries the model call's performance (TS
// onStepEnd event.performance); the legacy loop left it empty.
func TestR65GenerateAgentStepEndTelemetryCarriesPerformance(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		time.Sleep(30 * time.Millisecond)
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop, Usage: types.Usage{OutputTokens: intPtr(10)}}, nil
	}}
	rec := &r65StepEndRecorder{}
	_, err := NewToolLoopAgent(AgentConfig{
		Model:     model,
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.steps) != 1 {
		t.Fatalf("step ends = %d", len(rec.steps))
	}
	if p := rec.steps[0].Performance; p.ResponseTimeMs < 30 || p.EffectiveOutputTokensPerSecond <= 0 {
		t.Fatalf("step end performance = %+v, want the model call's", p)
	}
}
