package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r71genOrderLog struct {
	mu  sync.Mutex
	got []string
}

func (l *r71genOrderLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, s)
}

func (l *r71genOrderLog) check(t *testing.T, want []string) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.Join(l.got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", l.got, want)
	}
}

// r71genOrderTelemetry records the telemetry end events in the shared log.
type r71genOrderTelemetry struct {
	telemetry.NoopTelemetryIntegration
	log *r71genOrderLog
}

func (r *r71genOrderTelemetry) OnToolExecutionEnd(context.Context, telemetry.TelemetryToolCallFinishEvent) {
	r.log.add("telemetry.onToolExecutionEnd")
}

func (r *r71genOrderTelemetry) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	r.log.add("telemetry.onStepEnd")
}

func (r *r71genOrderTelemetry) OnStepError(context.Context, telemetry.TelemetryErrorEvent) {
	r.log.add("telemetry.onStepError")
}

func (r *r71genOrderTelemetry) OnFinish(context.Context, telemetry.TelemetryFinishEvent) {
	r.log.add("telemetry.onEnd")
}

// TS ToolLoopAgent runs generateText, whose notify([callback,
// telemetry.onX]) starts the user callback first: onToolExecutionEnd before
// the telemetry tool end, onStepFinish before the telemetry step end, and
// onEnd before the telemetry end.
func TestR71Gen_LegacyLoopCallbacksRunBeforeTelemetryEnds(t *testing.T) {
	log := &r71genOrderLog{}
	model := &mockLanguageModel{responses: []types.GenerateResult{
		{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls},
		{Text: "done", FinishReason: types.FinishReasonStop},
	}}
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
		Telemetry:          &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{&r71genOrderTelemetry{log: log}}},
		OnToolExecutionEnd: func(context.Context, ai.OnToolCallFinishEvent) { log.add("onToolExecutionEnd") },
		OnStepEndEvent:     func(context.Context, ai.OnStepFinishEvent) { log.add("onStepEnd") },
		OnEndEvent:         func(context.Context, ai.OnFinishEvent) { log.add("onEnd") },
	})
	if _, err := a.Execute(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	log.check(t, []string{
		"onToolExecutionEnd", "telemetry.onToolExecutionEnd",
		"onStepEnd", "telemetry.onStepEnd",
		"onStepEnd", "telemetry.onStepEnd",
		"onEnd", "telemetry.onEnd",
	})
}

// TS notify([onStepError, telemetry.onStepError]): a failed model call
// reaches the user OnStepError before the telemetry step error.
func TestR71Gen_LegacyLoopStepErrorRunsBeforeTelemetry(t *testing.T) {
	log := &r71genOrderLog{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, errors.New("boom")
		},
	}
	zero := 0
	a := NewToolLoopAgent(AgentConfig{
		Model:       model,
		MaxRetries:  &zero,
		Telemetry:   &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{&r71genOrderTelemetry{log: log}}},
		OnStepError: func(context.Context, ai.OnStepErrorEvent) { log.add("onStepError") },
	})
	if _, err := a.Execute(context.Background(), "go"); err == nil {
		t.Fatal("want an error")
	}
	log.check(t, []string{"onStepError", "telemetry.onStepError"})
}

// A failing ToModelOutput fails the run, but the tool's execution end events
// still fire first (TS converts the output only after executeToolCall's
// onToolExecutionEnd), the user callback before the telemetry.
func TestR71Gen_LegacyLoopToModelOutputErrorStillEndsTheExecution(t *testing.T) {
	log := &r71genOrderLog{}
	model := &mockLanguageModel{responses: []types.GenerateResult{
		{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls},
	}}
	var ended ai.OnToolCallFinishEvent
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "raw", nil
			},
			ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				return nil, errors.New("convert failed")
			}}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{&r71genOrderTelemetry{log: log}}},
		OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			ended = e
			log.add("onToolExecutionEnd")
		},
	})
	_, err := a.Execute(context.Background(), "go")
	if err == nil || !strings.Contains(err.Error(), "convert failed") {
		t.Fatalf("err = %v, want the conversion error", err)
	}
	log.check(t, []string{"onToolExecutionEnd", "telemetry.onToolExecutionEnd", "telemetry.onStepError"})
	if ended.ToolCallID != "call-1" || ended.ToolOutput.Result != "raw" || ended.ToolOutput.Error != nil || ended.ToolOutput.ModelOutput != nil {
		t.Fatalf("tool end event = %+v", ended)
	}
}
