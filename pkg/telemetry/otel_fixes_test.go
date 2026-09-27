package telemetry

import (
	"context"
	"errors"
	"math"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// findSpan looks up a span by name, checking still-open spans first (their
// ReadWriteSpan is a live view, so attributes set after OnStart are visible)
// and falling back to ended spans.
func findSpan(rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, s := range rec.Started() {
		if s.Name() == name {
			return s
		}
	}
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func attrValue(s sdktrace.ReadOnlySpan, key string) (interface{}, bool) {
	if s == nil {
		return nil, false
	}
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsInterface(), true
		}
	}
	return nil, false
}

// TestOTelIntegrationSkipsNonFiniteFloatAttributes covers OTEL-FIXES 50ab016:
// a NaN or +/-Inf tokens-per-second value (e.g. a zero-duration division)
// must not be sent to OTLP, which rejects non-finite floats.
func TestOTelIntegrationSkipsNonFiniteFloatAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{IsEnabled: Bool(true), Tracer: tracer}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "lm-1"})
	finiteVal := 12.5
	nonFinite := math.Inf(1)
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings: settings,
		CallID:   "lm-1",
		Performance: LanguageModelCallPerformance{
			// NaN/Inf here must be dropped, not sent as an attribute value.
			EffectiveOutputTokensPerSecond: math.NaN(),
			EffectiveTotalTokensPerSecond:  math.Inf(1),
			OutputTokensPerSecond:          &nonFinite,
			InputTokensPerSecond:           &finiteVal,
		},
	})

	span := findSpan(rec, "chat")
	if _, ok := attrValue(span, "ai.response.effectiveOutputTokensPerSecond"); ok {
		t.Fatal("expected NaN effectiveOutputTokensPerSecond to be dropped")
	}
	if _, ok := attrValue(span, "ai.response.effectiveTotalTokensPerSecond"); ok {
		t.Fatal("expected +Inf effectiveTotalTokensPerSecond to be dropped")
	}
	if _, ok := attrValue(span, "ai.response.outputTokensPerSecond"); ok {
		t.Fatal("expected +Inf outputTokensPerSecond to be dropped")
	}
	v, ok := attrValue(span, "ai.response.inputTokensPerSecond")
	if !ok || v.(float64) != finiteVal {
		t.Fatalf("expected the finite inputTokensPerSecond to be kept, got %v ok=%v", v, ok)
	}
}

// TestOTelIntegrationCustomSpanAttributesDropsNonFiniteFloats covers the
// EnrichSpan path of OTEL-FIXES 50ab016.
func TestOTelIntegrationCustomSpanAttributesDropsNonFiniteFloats(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{
		IsEnabled: Bool(true),
		Tracer:    tracer,
		EnrichSpan: func(context.Context, EnrichSpanOptions) map[string]interface{} {
			return map[string]interface{}{
				"custom.bad":        math.NaN(),
				"custom.bad_slice":  []float64{1, math.Inf(-1)},
				"custom.good":       2.5,
				"custom.good_slice": []float64{1, 2, 3},
			}
		},
	}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	trace.SpanFromContext(ctx).End()
	span := findSpan(rec, "ai.generateText")
	if _, ok := attrValue(span, "custom.bad"); ok {
		t.Fatal("expected NaN custom attribute to be dropped")
	}
	if _, ok := attrValue(span, "custom.bad_slice"); ok {
		t.Fatal("expected a float slice containing -Inf to be dropped entirely")
	}
	if v, ok := attrValue(span, "custom.good"); !ok || v.(float64) != 2.5 {
		t.Fatalf("expected the finite custom attribute to be kept, got %v ok=%v", v, ok)
	}
	if _, ok := attrValue(span, "custom.good_slice"); !ok {
		t.Fatal("expected the all-finite float slice to be kept")
	}
}

// TestOTelIntegrationRecordsHTTPStatusOnErrorSpan covers OTEL-FIXES 2dd0244:
// a *providererrors.ProviderError's status code (including one wrapped in a
// RetryError) must be recorded as http.response.status_code.
func TestOTelIntegrationRecordsHTTPStatusOnErrorSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{IsEnabled: Bool(true), Tracer: tracer}

	apiErr := providererrors.NewProviderError("openai", 503, "server_error", "boom", nil)
	retryErr := &providererrors.RetryError{Message: "retries exhausted", LastError: apiErr}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: retryErr})

	span := findSpan(rec, "ai.generateText")
	v, ok := attrValue(span, "http.response.status_code")
	if !ok || v.(int64) != 503 {
		t.Fatalf("expected http.response.status_code=503 unwrapped from RetryError, got %v ok=%v", v, ok)
	}
}

func TestOTelIntegrationDirectProviderErrorRecordsHTTPStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{IsEnabled: Bool(true), Tracer: tracer}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	integration.OnError(ctx, TelemetryErrorEvent{
		Settings: settings,
		Error:    providererrors.NewProviderError("anthropic", 429, "rate_limit", "slow down", nil),
	})
	span := findSpan(rec, "ai.generateText")
	v, ok := attrValue(span, "http.response.status_code")
	if !ok || v.(int64) != 429 {
		t.Fatalf("expected http.response.status_code=429, got %v ok=%v", v, ok)
	}
}

func TestOTelIntegrationNonProviderErrorOmitsHTTPStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{IsEnabled: Bool(true), Tracer: tracer}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("plain error")})
	span := findSpan(rec, "ai.generateText")
	if _, ok := attrValue(span, "http.response.status_code"); ok {
		t.Fatal("expected no http.response.status_code for a non-provider error")
	}
}

// TestOTelIntegrationRuntimeContextAttributesOnRootAndToolSpans covers
// OTEL-FIXES 1e200eb + 0651c5f: nested runtime context is flattened into
// ai.settings.context.* attributes on the root span, and the same flattened
// attrs are copied onto tool call spans.
func TestOTelIntegrationRuntimeContextAttributesOnRootAndToolSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{IsEnabled: Bool(true), Tracer: tracer}

	runtimeContext := map[string]interface{}{
		"userId": "u1",
		"nested": map[string]interface{}{
			"role":  "admin",
			"quiet": nil,
			"tags":  []string{"a", "b"},
		},
	}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType:  "ai.generateText",
		Settings:       settings,
		RuntimeContext: runtimeContext,
	})
	rootSpan := findSpan(rec, "ai.generateText")
	if v, ok := attrValue(rootSpan, "ai.settings.context.userId"); !ok || v.(string) != "u1" {
		t.Fatalf("expected ai.settings.context.userId=u1 on the root span, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "ai.settings.context.nested.role"); !ok || v.(string) != "admin" {
		t.Fatalf("expected flattened nested.role on the root span, got %v ok=%v", v, ok)
	}
	if _, ok := attrValue(rootSpan, "ai.settings.context.nested.quiet"); ok {
		t.Fatal("expected a nil nested value to be skipped")
	}
	if _, ok := attrValue(rootSpan, "ai.settings.context.nested.tags"); !ok {
		t.Fatal("expected an array value to be kept as-is (not further flattened)")
	}

	toolCtx := integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{Settings: settings, ToolCallID: "call-1", ToolName: "lookup"})
	integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{Settings: settings, ToolCallID: "call-1", ToolName: "lookup"})
	toolSpan := findSpan(rec, "ai.toolCall.lookup")
	if v, ok := attrValue(toolSpan, "ai.settings.context.userId"); !ok || v.(string) != "u1" {
		t.Fatalf("expected ai.settings.context.userId=u1 on the tool span, got %v ok=%v", v, ok)
	}
}
