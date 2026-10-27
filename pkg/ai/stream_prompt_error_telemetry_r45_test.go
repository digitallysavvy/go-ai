package ai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText standardizes the prompt before the start events, so an
// invalid prompt opens no span; Go opened the root span and its error path
// could fall back to (and end) the caller's span.
func TestStreamTextPromptErrorStartsNoTelemetry(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	settings := &TelemetrySettings{
		RecordInputs: true, RecordOutputs: true,
		Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")})},
	}
	ctx, caller := tp.Tracer("caller").Start(context.Background(), "caller")
	defer caller.End()

	stream, err := StreamText(ctx, StreamTextOptions{Model: &testutil.MockLanguageModel{}, Telemetry: settings})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.ReadAll(); err == nil {
		t.Fatal("expected a prompt error")
	}
	if n := len(rec.Started()); n != 1 {
		t.Fatalf("started %d spans, want only the caller's", n)
	}
	if n := len(rec.Ended()); n != 0 {
		t.Fatalf("%d spans ended, want none", n)
	}
}
