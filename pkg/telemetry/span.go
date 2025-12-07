package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SpanOptions configures a telemetry span
type SpanOptions struct {
	// Name is the operation name for the span
	Name string

	// Attributes are key-value pairs attached to the span
	Attributes []attribute.KeyValue

	// EndWhenDone controls whether the span should be ended automatically when the function returns
	EndWhenDone bool
}

// RecordSpan creates and executes a telemetry span for an operation.
// The span is automatically ended when the function completes, unless EndWhenDone is false.
// Errors are automatically recorded on the span.
func RecordSpan[T any](
	ctx context.Context,
	tracer trace.Tracer,
	opts SpanOptions,
	fn func(context.Context, trace.Span) (T, error),
) (T, error) {
	ctx, span := tracer.Start(ctx, opts.Name,
		trace.WithAttributes(opts.Attributes...),
	)

	result, err := fn(ctx, span)

	if err != nil {
		RecordErrorOnSpan(span, err)
		span.End()
		var zero T
		return zero, err
	}

	if opts.EndWhenDone {
		span.End()
	}

	return result, nil
}

// RecordErrorOnSpan records an error on a span and sets the span status to error.
func RecordErrorOnSpan(span trace.Span, err error) {
	if err == nil {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// GetBaseAttributes returns common attributes for AI operations.
func GetBaseAttributes(
	provider string,
	modelID string,
	settings *Settings,
	headers map[string]string,
) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("ai.model.provider", provider),
		attribute.String("ai.model.id", modelID),
	}

	// Add telemetry metadata
	if settings != nil {
		if settings.FunctionID != "" {
			attrs = append(attrs, attribute.String("ai.telemetry.functionId", settings.FunctionID))
		}

		for key, value := range settings.Metadata {
			attrs = append(attrs, attribute.KeyValue{
				Key:   attribute.Key("ai.telemetry.metadata." + key),
				Value: value,
			})
		}
	}

	// Add request headers (but avoid sensitive headers)
	for key, value := range headers {
		// Skip authorization and api key headers
		if key == "Authorization" || key == "x-api-key" || key == "api-key" {
			continue
		}
		attrs = append(attrs, attribute.String("ai.request.headers."+key, value))
	}

	return attrs
}

// AddSettingsAttributes adds model settings as attributes to a span.
func AddSettingsAttributes(span trace.Span, prefix string, settings map[string]interface{}) {
	for key, value := range settings {
		attrKey := prefix + "." + key
		switch v := value.(type) {
		case string:
			span.SetAttributes(attribute.String(attrKey, v))
		case int:
			span.SetAttributes(attribute.Int(attrKey, v))
		case int64:
			span.SetAttributes(attribute.Int64(attrKey, v))
		case float64:
			span.SetAttributes(attribute.Float64(attrKey, v))
		case bool:
			span.SetAttributes(attribute.Bool(attrKey, v))
		}
	}
}
