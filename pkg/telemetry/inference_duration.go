package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// InferenceDurationMetricName is the GenAI client inference duration
// histogram OpenTelemetry records once per model call of generateText and
// streamText (TS 59465581b0).
const InferenceDurationMetricName = "gen_ai.client.inference.duration"

// GenAIMeterName is the meter the inference duration histogram is created on.
const GenAIMeterName = "gen_ai"

// inferenceDurationBuckets are the GenAI semantic-convention bucket
// boundaries, in seconds (TS `explicitBucketBoundaries`).
var inferenceDurationBuckets = []float64{
	0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24,
	20.48, 40.96, 81.92,
}

// genAIInference is the open model call whose duration is still to be
// recorded (TS `state.inferenceMetrics`).
type genAIInference struct {
	start    time.Time
	provider string
	modelID  string
	// ctx carries the chat span (TS `state.inferenceContext`).
	ctx context.Context
}

// inferenceEnd describes how a model call ended.
type inferenceEnd struct {
	// responseTimeMs is the model call's own duration; nil measures from the
	// call start.
	responseTimeMs *int64
	// provider is the responding provider; empty uses the requested one.
	provider string
	// responseModelID is gen_ai.response.model; nil omits it.
	responseModelID *string
	// errorType is error.type; empty omits it.
	errorType string
}

// meterProvider returns the configured MeterProvider, or the global one
// resolved now, so a provider registered after the integration was built
// still receives metrics (TS resolves the meter at record time).
func (i OpenTelemetry) meterProvider() metric.MeterProvider {
	if i.opts.MeterProvider != nil {
		return i.opts.MeterProvider
	}
	return otel.GetMeterProvider()
}

// beginInference records the start of a model call for callID. A model call
// still open (a retried stream that started a new attempt without ending the
// failed one) is recorded first, as an `_OTHER` error.
func (i OpenTelemetry) beginInference(callID string, inference genAIInference) {
	st := genAILoadedState(i.stateKey(callID))
	if st == nil {
		return
	}
	i.recordInferenceDuration(callID, inferenceEnd{errorType: "_OTHER"})
	st.mu.Lock()
	st.inference = &inference
	st.mu.Unlock()
}

// recordInferenceDuration records the open model call of callID once, and
// does nothing when none is open. Mirrors TS `recordInferenceDuration`.
func (i OpenTelemetry) recordInferenceDuration(callID string, end inferenceEnd) {
	st := genAILoadedState(i.stateKey(callID))
	if st == nil {
		return
	}
	st.mu.Lock()
	inference := st.inference
	st.inference = nil
	st.mu.Unlock()
	if inference == nil {
		return
	}
	seconds := time.Since(inference.start).Seconds()
	if end.responseTimeMs != nil {
		seconds = float64(*end.responseTimeMs) / 1000
	}
	provider := end.provider
	if provider == "" {
		provider = inference.provider
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", mapProviderName(provider)),
		attribute.String("gen_ai.request.model", inference.modelID),
	}
	if end.responseModelID != nil {
		attrs = append(attrs, attribute.String("gen_ai.response.model", *end.responseModelID))
	}
	if end.errorType != "" {
		attrs = append(attrs, attribute.String("error.type", end.errorType))
	}
	histogram, err := i.meterProvider().Meter(GenAIMeterName).Float64Histogram(InferenceDurationMetricName,
		metric.WithDescription("GenAI client inference operation duration."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(inferenceDurationBuckets...),
	)
	if err != nil || histogram == nil {
		return
	}
	ctx := inference.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	histogram.Record(ctx, seconds, metric.WithAttributes(attrs...))
}

// inferenceErrorType is the error.type of a failed model call: the TS
// `error instanceof Error ? error.name : '_OTHER'`. Go errors carry no class
// name, so the SDK's named errors (ErrorName), cancellation ("AbortError"),
// deadlines ("TimeoutError"), provider API errors ("AI_APICallError") and
// retry errors ("AI_RetryError") are named, the first one found along the
// wrap chain, and any other error is "Error".
func inferenceErrorType(err error) string {
	if err == nil {
		return "_OTHER"
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if named, ok := e.(interface{ ErrorName() string }); ok && named.ErrorName() != "" {
			return named.ErrorName()
		}
		switch e.(type) {
		case *providererrors.ProviderError:
			return "AI_APICallError"
		case *providererrors.RetryError:
			return "AI_RetryError"
		}
		if e == context.Canceled {
			return "AbortError"
		}
		if e == context.DeadlineExceeded {
			return "TimeoutError"
		}
	}
	return "Error"
}
