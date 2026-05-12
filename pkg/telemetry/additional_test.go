package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

type eventSpy struct {
	mu sync.Mutex

	starts              int
	stepStarts          int
	toolStarts          int
	toolFinishes        int
	chunks              int
	stepFinishes        int
	finishes            int
	errs                int
	lmCallStarts        int
	lmCallEnds          int
	embedStarts         int
	embedFinishes       int
	rerankStarts        int
	rerankFinishes      int
	executeToolCalls    int
	lastExecuteToolName string
}

func (s *eventSpy) OnStart(ctx context.Context, _ TelemetryStartEvent) context.Context {
	s.mu.Lock()
	s.starts++
	s.mu.Unlock()
	return ctx
}
func (s *eventSpy) OnStepStart(ctx context.Context, _ TelemetryStepStartEvent) context.Context {
	s.mu.Lock()
	s.stepStarts++
	s.mu.Unlock()
	return ctx
}
func (s *eventSpy) OnToolCallStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	s.mu.Lock()
	s.toolStarts++
	s.mu.Unlock()
	return ctx
}
func (s *eventSpy) OnToolCallFinish(_ context.Context, _ TelemetryToolCallFinishEvent) {
	s.mu.Lock()
	s.toolFinishes++
	s.mu.Unlock()
}
func (s *eventSpy) OnChunk(_ context.Context, _ TelemetryChunkEvent) {
	s.mu.Lock()
	s.chunks++
	s.mu.Unlock()
}
func (s *eventSpy) OnStepFinish(_ context.Context, _ TelemetryStepFinishEvent) {
	s.mu.Lock()
	s.stepFinishes++
	s.mu.Unlock()
}
func (s *eventSpy) OnFinish(_ context.Context, _ TelemetryFinishEvent) {
	s.mu.Lock()
	s.finishes++
	s.mu.Unlock()
}
func (s *eventSpy) OnError(_ context.Context, _ TelemetryErrorEvent) {
	s.mu.Lock()
	s.errs++
	s.mu.Unlock()
}
func (s *eventSpy) ExecuteTool(
	ctx context.Context,
	toolName string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	s.mu.Lock()
	s.executeToolCalls++
	s.lastExecuteToolName = toolName
	s.mu.Unlock()
	return execute(ctx, args)
}

func (s *eventSpy) OnLanguageModelCallStart(_ context.Context, _ LanguageModelCallStartEvent) {
	s.mu.Lock()
	s.lmCallStarts++
	s.mu.Unlock()
}
func (s *eventSpy) OnLanguageModelCallEnd(_ context.Context, _ LanguageModelCallEndEvent) {
	s.mu.Lock()
	s.lmCallEnds++
	s.mu.Unlock()
}
func (s *eventSpy) OnEmbedStart(_ context.Context, _ EmbeddingModelCallStartEvent) {
	s.mu.Lock()
	s.embedStarts++
	s.mu.Unlock()
}
func (s *eventSpy) OnEmbedFinish(_ context.Context, _ EmbeddingModelCallEndEvent) {
	s.mu.Lock()
	s.embedFinishes++
	s.mu.Unlock()
}
func (s *eventSpy) OnRerankStart(_ context.Context, _ RerankingModelCallStartEvent) {
	s.mu.Lock()
	s.rerankStarts++
	s.mu.Unlock()
}
func (s *eventSpy) OnRerankFinish(_ context.Context, _ RerankingModelCallEndEvent) {
	s.mu.Lock()
	s.rerankFinishes++
	s.mu.Unlock()
}

func TestFireEventFanOutAndDiagnosticsWrappers(t *testing.T) {
	ClearTelemetryIntegrations()
	defer RegisterTelemetryIntegration(NoopTelemetryIntegration{})

	spy := &eventSpy{}
	RegisterTelemetryIntegration(spy)

	var typedStarts int
	unsub := SubscribeDiagnosticTyped(DiagnosticEventOnStart, func(_ context.Context, _ TelemetryStartEvent) error {
		typedStarts++
		return nil
	})
	defer unsub()

	settings := &Settings{IsEnabled: Bool(true)}
	ctx := FireOnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
	})
	ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{Settings: settings})
	FireOnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings})
	FireOnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings})
	FireOnEmbedStart(ctx, EmbeddingModelCallStartEvent{Settings: settings})
	FireOnEmbedFinish(ctx, EmbeddingModelCallEndEvent{Settings: settings})
	FireOnRerankStart(ctx, RerankingModelCallStartEvent{Settings: settings})
	FireOnRerankFinish(ctx, RerankingModelCallEndEvent{Settings: settings})
	ctx = FireOnToolCallStart(ctx, TelemetryToolCallStartEvent{Settings: settings})
	FireOnToolCallFinish(ctx, TelemetryToolCallFinishEvent{Settings: settings})
	FireOnChunk(ctx, TelemetryChunkEvent{Settings: settings, ChunkType: "text"})
	FireOnStepFinish(ctx, TelemetryStepFinishEvent{Settings: settings})
	FireOnFinish(ctx, TelemetryFinishEvent{Settings: settings})
	FireOnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom")})

	if typedStarts != 1 {
		t.Fatalf("typed diagnostic start events = %d, want 1", typedStarts)
	}

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.starts != 1 || spy.stepStarts != 1 || spy.toolStarts != 1 || spy.toolFinishes != 1 {
		t.Fatalf("unexpected basic counters: %#v", spy)
	}
	if spy.chunks != 1 || spy.stepFinishes != 1 || spy.finishes != 1 || spy.errs != 1 {
		t.Fatalf("unexpected finish counters: %#v", spy)
	}
	if spy.lmCallStarts != 1 || spy.lmCallEnds != 1 || spy.embedStarts != 1 || spy.embedFinishes != 1 {
		t.Fatalf("unexpected optional event counters: %#v", spy)
	}
	if spy.rerankStarts != 1 || spy.rerankFinishes != 1 {
		t.Fatalf("unexpected rerank counters: %#v", spy)
	}
}

func TestFireExecuteToolWithSettingsAndDisabled(t *testing.T) {
	ClearTelemetryIntegrations()
	defer RegisterTelemetryIntegration(NoopTelemetryIntegration{})
	spy := &eventSpy{}
	RegisterTelemetryIntegration(spy)

	val, err := FireExecuteToolWithSettings(
		context.Background(),
		&Settings{IsEnabled: Bool(true)},
		"lookup_weather",
		map[string]interface{}{"city": "Paris"},
		func(_ context.Context, args map[string]interface{}) (interface{}, error) {
			return args["city"], nil
		},
	)
	if err != nil || val != "Paris" {
		t.Fatalf("FireExecuteToolWithSettings result=(%v,%v)", val, err)
	}

	spy.mu.Lock()
	if spy.executeToolCalls == 0 || spy.lastExecuteToolName != "lookup_weather" {
		spy.mu.Unlock()
		t.Fatalf("execute tool not invoked via integration: %#v", spy)
	}
	spy.mu.Unlock()

	disabledVal, err := FireExecuteToolWithSettings(
		context.Background(),
		&Settings{IsEnabled: Bool(false)},
		"x",
		nil,
		func(_ context.Context, _ map[string]interface{}) (interface{}, error) { return "ok", nil },
	)
	if err != nil || disabledVal != "ok" {
		t.Fatalf("disabled execute result=(%v,%v)", disabledVal, err)
	}
}

func TestSettingsHelpersAndSpanUtilities(t *testing.T) {
	s := DefaultSettings().
		WithEnabled(false).
		WithRecordInputs(false).
		WithRecordOutputs(false).
		WithFunctionID("fn")
	s = s.WithTracer(trace.NewNoopTracerProvider().Tracer("test"))

	if Enabled(s) {
		t.Fatal("settings should be disabled after WithEnabled(false)")
	}
	if s.RecordInputs || s.RecordOutputs || s.FunctionID != "fn" || s.Tracer == nil {
		t.Fatalf("unexpected settings copy: %#v", s)
	}

	attrs := GetBaseAttributes("openai", "gpt-5", s, map[string]string{
		"Authorization": "secret",
		"x-api-key":     "secret",
		"api-key":       "secret",
		"X-Trace":       "abc",
	})
	if len(attrs) < 3 {
		t.Fatalf("expected model + function + safe header attrs, got %#v", attrs)
	}

	tracer := trace.NewNoopTracerProvider().Tracer("test")
	got, err := RecordSpan(context.Background(), tracer, SpanOptions{Name: "x", EndWhenDone: true}, func(context.Context, trace.Span) (string, error) {
		return "ok", nil
	})
	if err != nil || got != "ok" {
		t.Fatalf("RecordSpan success result=(%v,%v)", got, err)
	}
	if _, err := RecordSpan(context.Background(), tracer, SpanOptions{Name: "x", EndWhenDone: false}, func(context.Context, trace.Span) (string, error) {
		return "", errors.New("fail")
	}); err == nil {
		t.Fatal("RecordSpan should return function error")
	}
}

func TestDefaultDiagnosticChannelWrappers(t *testing.T) {
	ch := DefaultDiagnosticChannel()
	if ch == nil {
		t.Fatal("DefaultDiagnosticChannel should not be nil")
	}

	var count int
	unsub := SubscribeDiagnostic(func(_ context.Context, msg DiagnosticMessage) error {
		if msg.Type == DiagnosticEventOnFinish {
			count++
		}
		return nil
	})
	PublishDiagnostic(context.Background(), DiagnosticEventOnFinish, TelemetryFinishEvent{})
	unsub()
	if count != 1 {
		t.Fatalf("diagnostic wrapper count = %d, want 1", count)
	}
}
