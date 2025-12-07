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

// GetTracer returns an appropriate tracer based on the settings.
// If telemetry is disabled, returns a no-op tracer.
// If a custom tracer is provided in settings, returns that.
// Otherwise, returns the global tracer.
func GetTracer(settings *Settings) trace.Tracer {
	if settings == nil || !settings.IsEnabled {
		return noop.NewTracerProvider().Tracer(TracerName)
	}

	if settings.Tracer != nil {
		return settings.Tracer
	}

	return otel.Tracer(TracerName)
}
