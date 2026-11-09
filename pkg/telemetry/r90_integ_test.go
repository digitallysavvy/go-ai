package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// carry r90 open item: TS onError records `event.error ?? event` on the root
// span; with no error that is the event object (not an Error), so the span
// still gets an ERROR status, without a message or an exception event
// (open-telemetry.ts 2330-2360, legacy-open-telemetry.ts 1480-1520,
// recordErrorOnSpan / recordSpanError). Go left the status unset.
func TestR90integOnErrorWithoutErrorSetsErrorStatus(t *testing.T) {
	for name, mk := range map[string]func(tp *sdktrace.TracerProvider) TelemetryIntegration{
		"genai": func(tp *sdktrace.TracerProvider) TelemetryIntegration {
			return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		},
		"legacy": func(tp *sdktrace.TracerProvider) TelemetryIntegration {
			return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		},
	} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		integration := mk(tp)
		settings := &Settings{IsEnabled: Bool(true)}
		ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
		integration.OnError(ctx, TelemetryErrorEvent{Settings: settings})
		ended := rec.Ended()
		if len(ended) != 1 {
			t.Fatalf("%s: ended spans = %d, want 1", name, len(ended))
		}
		if st := ended[0].Status(); st.Code != codes.Error || st.Description != "" || len(ended[0].Events()) != 0 {
			t.Errorf("%s: status = %+v, events %d; want ERROR without message or exception", name, st, len(ended[0].Events()))
		}
		_ = tp.Shutdown(context.Background())
	}
}
