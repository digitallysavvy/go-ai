package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	// TracerName is the name used for the AI SDK tracer
	TracerName = "ai-sdk"
)

// GetTracer returns the global tracer, or a no-op tracer when telemetry is
// explicitly disabled. This is the legacy fallback used by LegacyOpenTelemetry
// when it was not constructed with its own tracer (NewLegacyOpenTelemetry);
// new code should configure a tracer on the integration itself
// (LegacyOpenTelemetryOptions.Tracer / OpenTelemetryOptions.Tracer) rather
// than through Options, which no longer carries a Tracer field (9b47dea):
// telemetry.Options.Tracer/WithTracer were removed because a single
// process-wide field could not express "each integration gets its own
// tracer" once more than one integration can be registered at once.
func GetTracer(settings *Settings) trace.Tracer {
	if !Enabled(settings) {
		return noop.NewTracerProvider().Tracer(TracerName)
	}
	return otel.Tracer(TracerName)
}
