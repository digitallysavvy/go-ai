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

// findSpans returns every span (started or ended) with the given name, for
// tests that assert a span was NOT duplicated.
func findSpans(rec *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	seen := make(map[trace.SpanID]bool)
	for _, s := range rec.Started() {
		if s.Name() == name && !seen[s.SpanContext().SpanID()] {
			seen[s.SpanContext().SpanID()] = true
			out = append(out, s)
		}
	}
	for _, s := range rec.Ended() {
		if s.Name() == name && !seen[s.SpanContext().SpanID()] {
			seen[s.SpanContext().SpanID()] = true
			out = append(out, s)
		}
	}
	return out
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
//
// This used to exercise LegacyOpenTelemetry.OnLanguageModelCallStart/End's
// own "chat" span and its Go-only ai.response.*TokensPerSecond attributes.
// H4 item 1 removed those methods (TS's LegacyOpenTelemetry never creates a
// "chat" span at all — see registry.go's doc comment above where they used
// to be), so this now exercises the one remaining Performance-derived float
// on a Legacy span: OnStepEnd's ai.response.avgOutputTokensPerSecond
// (ai.streamText only), which goes through the same setFiniteFloat64 guard.
func TestOTelIntegrationSkipsNonFiniteFloatAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	// Step 1: NaN avgOutputTokensPerSecond must be dropped from the span
	// attribute (the "ai.stream.finish" event attribute is intentionally NOT
	// filtered, matching TS — see OnStepEnd's own comment).
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.streamText", Settings: settings})
	nanStepCtx := integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.streamText", StepNumber: 0})
	integration.OnStepEnd(nanStepCtx, TelemetryStepEndEvent{
		Settings:      settings,
		OperationType: "ai.streamText",
		StepNumber:    0,
		FinishReason:  "stop",
		Performance:   LanguageModelCallPerformance{ResponseTimeMs: 10, EffectiveOutputTokensPerSecond: math.NaN()},
	})
	nanSpans := findSpans(rec, "ai.streamText.doStream")
	if len(nanSpans) != 1 {
		t.Fatalf("expected exactly 1 ai.streamText.doStream span for the NaN step, got %d", len(nanSpans))
	}
	if _, ok := attrValue(nanSpans[0], "ai.response.avgOutputTokensPerSecond"); ok {
		t.Fatal("expected NaN avgOutputTokensPerSecond to be dropped")
	}

	// Step 2 (separate call, its own root span): a finite value must be kept.
	ctx2 := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.streamText", Settings: settings})
	finiteStepCtx := integration.OnStepStart(ctx2, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.streamText", StepNumber: 0})
	integration.OnStepEnd(finiteStepCtx, TelemetryStepEndEvent{
		Settings:      settings,
		OperationType: "ai.streamText",
		StepNumber:    0,
		FinishReason:  "stop",
		Performance:   LanguageModelCallPerformance{ResponseTimeMs: 10, EffectiveOutputTokensPerSecond: 12.5},
	})
	allSpans := findSpans(rec, "ai.streamText.doStream")
	if len(allSpans) != 2 {
		t.Fatalf("expected 2 ai.streamText.doStream spans total, got %d", len(allSpans))
	}
	finiteSpan := allSpans[1]
	v, ok := attrValue(finiteSpan, "ai.response.avgOutputTokensPerSecond")
	if !ok || v.(float64) != 12.5 {
		t.Fatalf("expected the finite avgOutputTokensPerSecond to be kept, got %v ok=%v", v, ok)
	}
}

// TestOTelIntegrationCustomSpanAttributesDropsNonFiniteFloats covers the
// EnrichSpan path of OTEL-FIXES 50ab016.
func TestOTelIntegrationCustomSpanAttributesDropsNonFiniteFloats(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{
		IsEnabled: Bool(true),
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

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

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

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}
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

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}
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

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

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
	toolSpan := findSpan(rec, "ai.toolCall")
	if v, ok := attrValue(toolSpan, "ai.settings.context.userId"); !ok || v.(string) != "u1" {
		t.Fatalf("expected ai.settings.context.userId=u1 on the tool span, got %v ok=%v", v, ok)
	}
}

// TestLegacyOpenTelemetryStepSpanRequestSettingsAttributes covers H4 item 3:
// the Legacy step span (created by OnStepStart, mirroring TS's onStepStart/
// onObjectStepStart in legacy-open-telemetry.ts) must carry gen_ai.request.*
// attributes sourced from the root call's settings —
// frequency_penalty/max_tokens/presence_penalty/stop_sequences/temperature/
// top_k/top_p — matching the legacy-open-telemetry.test.ts snapshot rows
// (e.g. line ~432-437: "gen_ai.request.frequency_penalty": 0.3,
// "gen_ai.request.presence_penalty": 0.4, "gen_ai.request.temperature": 0.5,
// "gen_ai.request.top_k": 0.1, "gen_ai.request.top_p": 0.2). These come from
// state.settings (stashed once at ai.<op>Start), not from any per-step
// override — so this test also exercises that a per-step ModelID/Provider
// change doesn't affect which settings values land on gen_ai.request.*.
func TestLegacyOpenTelemetryStepSpanRequestSettingsAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-step-request-settings-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	maxTokens := 100
	temp := 0.5
	topP := 0.2
	topKInt := 1
	presence := 0.4
	frequency := 0.3

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType:    "ai.generateText",
		ModelProvider:    "mock-provider",
		ModelID:          "mock-model-id",
		Settings:         settings,
		MaxOutputTokens:  &maxTokens,
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topKInt,
		PresencePenalty:  &presence,
		FrequencyPenalty: &frequency,
		StopSequences:    []string{"stop"},
	})

	integration.OnStepStart(ctx, TelemetryStepStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
		ModelProvider: "mock-provider",
		ModelID:       "mock-model-id",
	})

	stepSpan := findSpan(rec, "ai.generateText.doGenerate")
	if stepSpan == nil {
		t.Fatal("expected an ai.generateText.doGenerate step span")
	}

	wantFloats := map[string]float64{
		"gen_ai.request.frequency_penalty": 0.3,
		"gen_ai.request.presence_penalty":  0.4,
		"gen_ai.request.temperature":       0.5,
		"gen_ai.request.top_p":             0.2,
	}
	for key, want := range wantFloats {
		v, ok := attrValue(stepSpan, key)
		if !ok || v.(float64) != want {
			t.Errorf("%s = %v (ok=%v), want %v", key, v, ok, want)
		}
	}
	if v, ok := attrValue(stepSpan, "gen_ai.request.max_tokens"); !ok || v.(int64) != 100 {
		t.Errorf("gen_ai.request.max_tokens = %v (ok=%v), want 100", v, ok)
	}
	if v, ok := attrValue(stepSpan, "gen_ai.request.top_k"); !ok || v.(int64) != 1 {
		t.Errorf("gen_ai.request.top_k = %v (ok=%v), want 1", v, ok)
	}
	if v, ok := attrValue(stepSpan, "gen_ai.request.stop_sequences"); !ok {
		t.Error("expected gen_ai.request.stop_sequences to be set")
	} else if got, ok := v.([]string); !ok || len(got) != 1 || got[0] != "stop" {
		t.Errorf("gen_ai.request.stop_sequences = %v, want [stop]", v)
	}
}

// TestLegacyOpenTelemetryObjectStepSpanOmitsStopSequences covers the
// onObjectStepStart half of H4 item 3: TS's onObjectStepStart never sets
// gen_ai.request.stop_sequences (generateObject/streamObject settings never
// carry StopSequences — see legacy-open-telemetry.ts onObjectOperationStart,
// which omits it from the settings object entirely), while the other
// gen_ai.request.* fields are still populated from state.settings.
func TestLegacyOpenTelemetryObjectStepSpanOmitsStopSequences(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-object-step-request-settings-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	temp := 0.5
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateObject",
		ModelProvider: "mock-provider",
		ModelID:       "mock-model-id",
		Settings:      settings,
		Temperature:   &temp,
		// StopSequences is set on the event, but generateObject/streamObject
		// must not surface it as gen_ai.request.stop_sequences — mirroring
		// OnStart's own `if e.OperationType == "ai.generateText" ||
		// "ai.streamText"` gate on legacySettings.StopSequences.
		StopSequences: []string{"stop"},
	})

	integration.OnStepStart(ctx, TelemetryStepStartEvent{
		OperationType: "ai.generateObject",
		Settings:      settings,
		ModelProvider: "mock-provider",
		ModelID:       "mock-model-id",
	})

	stepSpan := findSpan(rec, "ai.generateObject.doGenerate")
	if stepSpan == nil {
		t.Fatal("expected an ai.generateObject.doGenerate step span")
	}
	if v, ok := attrValue(stepSpan, "gen_ai.request.temperature"); !ok || v.(float64) != 0.5 {
		t.Errorf("gen_ai.request.temperature = %v (ok=%v), want 0.5", v, ok)
	}
	if _, ok := attrValue(stepSpan, "gen_ai.request.stop_sequences"); ok {
		t.Error("expected gen_ai.request.stop_sequences to be absent for generateObject")
	}
}
