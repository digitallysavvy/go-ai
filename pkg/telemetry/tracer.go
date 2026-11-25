package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	// LegacyTracerName names the global tracer LegacyOpenTelemetry uses when
	// it has no tracer of its own, as TS `trace.getTracer('ai')`.
	LegacyTracerName = "ai"

	// GenAITracerName names the global tracer the GenAI OpenTelemetry
	// integration uses when it has no tracer of its own, as TS
	// `trace.getTracer('gen_ai')`.
	GenAITracerName = "gen_ai"

	// TracerName was the name of the SDK's default tracer through v0.5.x.
	//
	// Deprecated: the SDK no longer uses it. LegacyOpenTelemetry and
	// GetTracer default to LegacyTracerName ("ai"), and OpenTelemetry to
	// GenAITracerName ("gen_ai"), as TS does.
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
//
// The global tracer is named LegacyTracerName ("ai"), as TS getTracer.
func GetTracer(settings *Settings) trace.Tracer {
	if !Enabled(settings) {
		return noop.NewTracerProvider().Tracer(LegacyTracerName)
	}
	return otel.Tracer(LegacyTracerName)
}
