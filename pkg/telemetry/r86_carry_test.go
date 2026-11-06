package telemetry

import (
	"context"
	"math"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// r86carryStartWithTracer is r85carryStart with a caller-chosen tracer.
func r86carryStartWithTracer(t *testing.T, tracer trace.Tracer, mp metric.MeterProvider, op string) *r85carryCall {
	t.Helper()
	c := &r85carryCall{t: t, i: NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer, MeterProvider: mp}), id: t.Name(), set: &Settings{IsEnabled: Bool(true)}, model: "gpt-4"}
	c.ctx = c.i.OnStart(context.Background(), TelemetryStartEvent{OperationType: op, Settings: c.set, CallID: c.id, ModelID: c.model, ModelProvider: "openai.chat"})
	c.ctx = c.i.OnStepStart(c.ctx, TelemetryStepStartEvent{Settings: c.set, OperationType: op, CallID: c.id})
	return c
}

// TS OpenTelemetry keeps its call state, step and chat spans whether or not
// the root span records (an unsampled trace, or no tracer provider: the spans
// are non-recording but present), so recordInferenceDuration still runs and
// gen_ai.client.inference.duration gets one point per model call. Go skipped
// every handler for a non-recording span, so a sampled-out call (or a
// metrics-only setup) recorded nothing.
func TestR86carryInferenceDurationWithoutRecordingSpans(t *testing.T) {
	never := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	t.Cleanup(func() { _ = never.Shutdown(context.Background()) })
	tracers := map[string]trace.Tracer{
		"never sampled": never.Tracer("g"),
		"noop tracer":   tracenoop.NewTracerProvider().Tracer("g"),
	}
	for name, tracer := range tracers {
		t.Run(name+" call end", func(t *testing.T) {
			mp := &r85carryMeterProvider{}
			c := r86carryStartWithTracer(t, tracer, mp, "ai.generateText")
			c.callStart()
			c.callEnd(1500, "stop", "resp-model")
			c.i.OnStepEnd(c.ctx, TelemetryStepEndEvent{Settings: c.set, CallID: c.id, FinishReason: "stop"})
			c.i.OnEnd(c.ctx, TelemetryFinishEvent{Settings: c.set, CallID: c.id})
			points := mp.recorded()
			if len(points) != 1 || math.Abs(points[0].value-1.5) > 1e-9 || points[0].attrs["gen_ai.response.model"] != "resp-model" {
				t.Fatalf("points = %+v, want one 1.5s point", points)
			}
		})
		t.Run(name+" step end after error", func(t *testing.T) {
			mp := &r85carryMeterProvider{}
			c := r86carryStartWithTracer(t, tracer, mp, "ai.streamText")
			c.callStart()
			c.i.OnStepEnd(c.ctx, TelemetryStepEndEvent{Settings: c.set, CallID: c.id, FinishReason: "error"})
			if points := mp.recorded(); len(points) != 1 || points[0].attrs["error.type"] != "_OTHER" {
				t.Fatalf("points = %+v", points)
			}
		})
		t.Run(name+" abort", func(t *testing.T) {
			mp := &r85carryMeterProvider{}
			c := r86carryStartWithTracer(t, tracer, mp, "ai.streamText")
			c.callStart()
			c.i.OnAbort(c.ctx, TelemetryAbortEvent{CallID: c.id})
			if points := mp.recorded(); len(points) != 1 || points[0].attrs["error.type"] != "AbortError" {
				t.Fatalf("points = %+v", points)
			}
		})
		t.Run(name+" object call", func(t *testing.T) {
			mp := &r85carryMeterProvider{}
			c := r86carryStartWithTracer(t, tracer, mp, "ai.generateObject")
			c.callStart()
			c.callEnd(5, "stop", "gpt-4")
			if points := mp.recorded(); len(points) != 0 {
				t.Fatalf("points = %+v, want none", points)
			}
		})
	}
}
