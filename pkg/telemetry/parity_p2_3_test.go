package telemetry

import (
	"context"
	"testing"
)

type executionAliasSpy struct {
	starts int
	ends   int
}

func (s *executionAliasSpy) OnStart(ctx context.Context, _ TelemetryStartEvent) context.Context {
	return ctx
}
func (s *executionAliasSpy) OnStepStart(ctx context.Context, _ TelemetryStepStartEvent) context.Context {
	return ctx
}
func (s *executionAliasSpy) OnToolCallStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	return ctx
}
func (s *executionAliasSpy) OnToolCallFinish(context.Context, TelemetryToolCallFinishEvent) {}
func (s *executionAliasSpy) OnToolExecutionStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	s.starts++
	return ctx
}
func (s *executionAliasSpy) OnToolExecutionEnd(context.Context, TelemetryToolCallFinishEvent) {
	s.ends++
}
func (s *executionAliasSpy) OnChunk(context.Context, TelemetryChunkEvent)           {}
func (s *executionAliasSpy) OnStepFinish(context.Context, TelemetryStepFinishEvent) {}
func (s *executionAliasSpy) OnFinish(context.Context, TelemetryFinishEvent)         {}
func (s *executionAliasSpy) OnError(context.Context, TelemetryErrorEvent)           {}
func (s *executionAliasSpy) ExecuteTool(ctx context.Context, _ string, args map[string]interface{}, execute func(context.Context, map[string]interface{}) (interface{}, error)) (interface{}, error) {
	return execute(ctx, args)
}

func TestFireOnToolCallDispatchesExecutionAliasMethods(t *testing.T) {
	ClearTelemetryIntegrations()
	defer RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	spy := &executionAliasSpy{}
	AddTelemetryIntegration(spy)
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := FireOnToolCallStart(context.Background(), TelemetryToolCallStartEvent{Settings: settings})
	FireOnToolCallFinish(ctx, TelemetryToolCallFinishEvent{Settings: settings})

	if spy.starts != 1 {
		t.Fatalf("OnToolExecutionStart calls = %d, want 1", spy.starts)
	}
	if spy.ends != 1 {
		t.Fatalf("OnToolExecutionEnd calls = %d, want 1", spy.ends)
	}
}
