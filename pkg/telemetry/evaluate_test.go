package telemetry

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestLegacyOpenTelemetryEvaluateSpans mirrors TypeScript's
// onEvaluateOperationStart / experimental_onEvaluationModelCallStart /
// experimental_onEvaluationModelCallEnd / onEvaluateOperationEnd in
// packages/otel/src/legacy-open-telemetry.ts: the root "ai.evaluate" span
// carries input-gated ai.evaluation.state/questions and an output-gated
// ai.evaluation.answers, and the nested "ai.evaluate.doEvaluate" span
// additionally carries usage and providerMetadata (ungated). Ported from
// pkg/ai/telemetry_test.go's former TestExperimentalEvaluate_Telemetry,
// now exercising the LegacyOpenTelemetry integration directly instead of
// going through ExperimentalEvaluate.
func TestLegacyOpenTelemetryEvaluateSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("evaluate-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{
		IsEnabled:     Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "evaluate-test",
	}

	questions := map[string]interface{}{
		"q1": map[string]interface{}{"type": "boolean", "instructions": "Is it correct?"},
	}
	state := map[string]interface{}{"input": "hello"}

	ctx := integration.OnEvaluateStart(context.Background(), EvaluateStartEvent{
		Settings:      settings,
		CallID:        "call-1",
		OperationID:   "ai.evaluate",
		ModelProvider: "test-provider",
		ModelID:       "test-model",
		State:         state,
		Questions:     questions,
		MaxRetries:    2,
	})

	integration.OnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{
		Settings:      settings,
		CallID:        "call-1",
		OperationID:   "ai.evaluate.doEvaluate",
		ModelProvider: "test-provider",
		ModelID:       "test-model",
		State:         state,
		Questions:     questions,
	})

	answers := map[string]interface{}{
		"q1": map[string]interface{}{"type": "boolean", "probability": 0.9},
	}
	inputTokens, outputTokens := int64(3), int64(7)
	integration.OnEvaluationModelCallEnd(ctx, EvaluationModelCallEndEvent{
		EvaluationModelCallStartEvent: EvaluationModelCallStartEvent{
			Settings:      settings,
			CallID:        "call-1",
			OperationID:   "ai.evaluate.doEvaluate",
			ModelProvider: "test-provider",
			ModelID:       "test-model",
			State:         state,
			Questions:     questions,
		},
		Answers:          answers,
		Usage:            TelemetryUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
		ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"key": "value"}},
	})

	integration.OnEvaluateEnd(ctx, EvaluateEndEvent{
		EvaluateStartEvent: EvaluateStartEvent{
			Settings:      settings,
			CallID:        "call-1",
			OperationID:   "ai.evaluate",
			ModelProvider: "test-provider",
			ModelID:       "test-model",
		},
		Answers: answers,
	})

	rootSpan := findSpan(rec, "ai.evaluate.evaluate-test")
	if rootSpan == nil {
		t.Fatal("expected a 'ai.evaluate.evaluate-test' root span")
	}
	doEvaluateSpan := findSpan(rec, "ai.evaluate.doEvaluate")
	if doEvaluateSpan == nil {
		t.Fatal("expected an 'ai.evaluate.doEvaluate' nested span")
	}

	if v, ok := attrValue(rootSpan, "ai.operationId"); !ok || v.(string) != "ai.evaluate" {
		t.Errorf("root ai.operationId = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "ai.evaluation.state"); !ok || v.(string) != `{"input":"hello"}` {
		t.Errorf("root ai.evaluation.state = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(rootSpan, "ai.evaluation.questions"); !ok {
		t.Error("root span missing ai.evaluation.questions")
	}
	if _, ok := attrValue(rootSpan, "ai.evaluation.answers"); !ok {
		t.Error("root span missing ai.evaluation.answers")
	}
	if v, ok := attrValue(rootSpan, "gen_ai.system"); !ok || v.(string) != "test-provider" {
		t.Errorf("root gen_ai.system = %v, ok=%v", v, ok)
	}

	if v, ok := attrValue(doEvaluateSpan, "ai.operationId"); !ok || v.(string) != "ai.evaluate.doEvaluate" {
		t.Errorf("doEvaluate ai.operationId = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(doEvaluateSpan, "ai.evaluation.state"); !ok {
		t.Error("doEvaluate span missing ai.evaluation.state")
	}
	if _, ok := attrValue(doEvaluateSpan, "ai.evaluation.answers"); !ok {
		t.Error("doEvaluate span missing ai.evaluation.answers")
	}
	if v, ok := attrValue(doEvaluateSpan, "ai.usage.inputTokens"); !ok || v.(int64) != 3 {
		t.Errorf("doEvaluate ai.usage.inputTokens = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(doEvaluateSpan, "ai.usage.outputTokens"); !ok || v.(int64) != 7 {
		t.Errorf("doEvaluate ai.usage.outputTokens = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(doEvaluateSpan, "ai.response.providerMetadata"); !ok {
		t.Error("doEvaluate span missing ai.response.providerMetadata")
	}

	// Both spans must belong to the same trace (doEvaluate is a child of the
	// root, mirroring the nested-span relationship in evaluate.ts).
	if rootSpan.SpanContext().TraceID() != doEvaluateSpan.SpanContext().TraceID() {
		t.Error("expected root and doEvaluate spans to share a trace ID")
	}
}

// TestLegacyOpenTelemetryEvaluateSpans_RecordInputsFalse verifies
// ai.evaluation.state/questions are omitted from both spans when
// RecordInputs is false, matching selectAttributes()'s input gating.
func TestLegacyOpenTelemetryEvaluateSpans_RecordInputsFalse(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("evaluate-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: false, RecordOutputs: true}

	ctx := integration.OnEvaluateStart(context.Background(), EvaluateStartEvent{
		Settings:    settings,
		CallID:      "call-1",
		OperationID: "ai.evaluate",
		State:       "secret state",
		Questions:   map[string]interface{}{"q1": "secret question"},
	})
	integration.OnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{
		Settings:    settings,
		CallID:      "call-1",
		OperationID: "ai.evaluate.doEvaluate",
		State:       "secret state",
		Questions:   map[string]interface{}{"q1": "secret question"},
	})
	integration.OnEvaluationModelCallEnd(ctx, EvaluationModelCallEndEvent{
		EvaluationModelCallStartEvent: EvaluationModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.evaluate.doEvaluate"},
		Answers:                       map[string]interface{}{"q1": "answer"},
	})
	integration.OnEvaluateEnd(ctx, EvaluateEndEvent{
		EvaluateStartEvent: EvaluateStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.evaluate"},
		Answers:            map[string]interface{}{"q1": "answer"},
	})

	rootSpan := findSpan(rec, "ai.evaluate")
	doEvaluateSpan := findSpan(rec, "ai.evaluate.doEvaluate")
	if rootSpan == nil || doEvaluateSpan == nil {
		t.Fatal("expected both root and nested spans")
	}
	for _, s := range []sdktrace.ReadOnlySpan{rootSpan, doEvaluateSpan} {
		if _, ok := attrValue(s, "ai.evaluation.state"); ok {
			t.Errorf("span %s: expected ai.evaluation.state to be omitted when RecordInputs is false", s.Name())
		}
		if _, ok := attrValue(s, "ai.evaluation.questions"); ok {
			t.Errorf("span %s: expected ai.evaluation.questions to be omitted when RecordInputs is false", s.Name())
		}
	}
	// Answers (output) should still be recorded since RecordOutputs is true.
	if _, ok := attrValue(rootSpan, "ai.evaluation.answers"); !ok {
		t.Error("root span missing ai.evaluation.answers")
	}
}

// TestLegacyOpenTelemetryEvaluateSpans_RecordOutputsFalse verifies
// ai.evaluation.answers is omitted when RecordOutputs is false, while usage
// and providerMetadata (ungated) remain on the nested span.
func TestLegacyOpenTelemetryEvaluateSpans_RecordOutputsFalse(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("evaluate-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: false}

	ctx := integration.OnEvaluateStart(context.Background(), EvaluateStartEvent{
		Settings: settings, CallID: "call-1", OperationID: "ai.evaluate",
	})
	integration.OnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{
		Settings: settings, CallID: "call-1", OperationID: "ai.evaluate.doEvaluate",
	})
	inputTokens := int64(1)
	integration.OnEvaluationModelCallEnd(ctx, EvaluationModelCallEndEvent{
		EvaluationModelCallStartEvent: EvaluationModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.evaluate.doEvaluate"},
		Answers:                       map[string]interface{}{"q1": "answer"},
		Usage:                         TelemetryUsage{InputTokens: &inputTokens},
		ProviderMetadata:              map[string]interface{}{"p": "v"},
	})
	integration.OnEvaluateEnd(ctx, EvaluateEndEvent{
		EvaluateStartEvent: EvaluateStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.evaluate"},
		Answers:            map[string]interface{}{"q1": "answer"},
	})

	rootSpan := findSpan(rec, "ai.evaluate")
	doEvaluateSpan := findSpan(rec, "ai.evaluate.doEvaluate")
	if rootSpan == nil || doEvaluateSpan == nil {
		t.Fatal("expected both root and nested spans")
	}
	if _, ok := attrValue(rootSpan, "ai.evaluation.answers"); ok {
		t.Error("root span: expected ai.evaluation.answers to be omitted when RecordOutputs is false")
	}
	if _, ok := attrValue(doEvaluateSpan, "ai.evaluation.answers"); ok {
		t.Error("doEvaluate span: expected ai.evaluation.answers to be omitted when RecordOutputs is false")
	}
	// usage and providerMetadata are unconditional (not output-gated).
	if v, ok := attrValue(doEvaluateSpan, "ai.usage.inputTokens"); !ok || v.(int64) != 1 {
		t.Errorf("doEvaluate ai.usage.inputTokens = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(doEvaluateSpan, "ai.response.providerMetadata"); !ok {
		t.Error("doEvaluate span missing ai.response.providerMetadata")
	}
}

// TestLegacyOpenTelemetryEvaluateSpans_ErrorClosesNestedSpan verifies the
// defensive behavior called out in the follow-up: since
// experimental_onEvaluationModelCallEnd is never notified on an error path
// (mirrors evaluate.ts), OnError must find and close the nested
// "ai.evaluate.doEvaluate" span via CallID so it never leaks, in addition to
// closing the root span.
func TestLegacyOpenTelemetryEvaluateSpans_ErrorClosesNestedSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("evaluate-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnEvaluateStart(context.Background(), EvaluateStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.evaluate",
	})
	integration.OnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.evaluate.doEvaluate",
	})

	// No OnEvaluationModelCallEnd notified — go straight to OnError, as
	// evaluate.go does on a failed DoEvaluate call or answer validation error.
	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom"), CallID: "call-err"})

	rootSpan := findSpan(rec, "ai.evaluate")
	doEvaluateSpan := findSpan(rec, "ai.evaluate.doEvaluate")
	if rootSpan == nil {
		t.Fatal("expected root span")
	}
	if doEvaluateSpan == nil {
		t.Fatal("expected nested doEvaluate span to have been recorded before being closed")
	}
	if rootSpan.EndTime().IsZero() {
		t.Error("expected root span to be ended by OnError")
	}
	if doEvaluateSpan.EndTime().IsZero() {
		t.Error("expected nested doEvaluate span to be defensively closed by OnError")
	}

	// The span must not be double-closeable / re-found via the same CallID
	// (LoadAndDelete semantics) — a second OnError call must not panic.
	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom again"), CallID: "call-err"})
}

// TestOpenTelemetryEvaluateSpans_ExperimentalEvaluationGate verifies the
// GenAI OpenTelemetry integration emits gen_ai.* attributes unconditionally
// but gates ai.evaluation.state/questions/answers behind
// OpenTelemetryOptions.ExperimentalEvaluation, matching TS's
// selectSupplementalAttributes(telemetry, this.supplementalAttributes, {
// experimental_evaluation: {...} }) in otel/src/open-telemetry.ts. P1-7b
// left this option inert because evaluate didn't exist yet; this closes
// that gap.
func TestOpenTelemetryEvaluateSpans_ExperimentalEvaluationGate(t *testing.T) {
	for _, gated := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[gated], func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			tracer := tp.Tracer("genai-evaluate-test")

			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer, ExperimentalEvaluation: gated, ProviderMetadata: true})
			settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

			ctx := integration.OnEvaluateStart(context.Background(), EvaluateStartEvent{
				Settings:      settings,
				CallID:        "call-1",
				OperationID:   "ai.evaluate",
				ModelProvider: "openai.chat",
				ModelID:       "gpt-5",
				State:         "hello",
				Questions:     map[string]interface{}{"q1": "is it correct?"},
			})
			integration.OnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{
				Settings:      settings,
				CallID:        "call-1",
				OperationID:   "ai.evaluate.doEvaluate",
				ModelProvider: "openai.chat",
				ModelID:       "gpt-5",
				State:         "hello",
				Questions:     map[string]interface{}{"q1": "is it correct?"},
			})
			inputTokens := int64(5)
			integration.OnEvaluationModelCallEnd(ctx, EvaluationModelCallEndEvent{
				EvaluationModelCallStartEvent: EvaluationModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.evaluate.doEvaluate"},
				Answers:                       map[string]interface{}{"q1": "yes"},
				Usage:                         TelemetryUsage{InputTokens: &inputTokens},
				ProviderMetadata:              map[string]interface{}{"p": "v"},
			})
			integration.OnEvaluateEnd(ctx, EvaluateEndEvent{
				EvaluateStartEvent: EvaluateStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.evaluate"},
				Answers:            map[string]interface{}{"q1": "yes"},
			})

			rootSpan := findSpan(rec, "evaluate gpt-5")
			spans := findSpans(rec, "evaluate gpt-5")
			if len(spans) != 2 {
				t.Fatalf("expected 2 'evaluate gpt-5' spans (root + nested), got %d", len(spans))
			}
			if rootSpan == nil {
				t.Fatal("expected a root 'evaluate gpt-5' span")
			}
			if v, ok := attrValue(rootSpan, "gen_ai.operation.name"); !ok || v.(string) != "evaluate" {
				t.Fatalf("expected gen_ai.operation.name=evaluate, got %v ok=%v", v, ok)
			}
			if v, ok := attrValue(rootSpan, "gen_ai.provider.name"); !ok || v.(string) != "openai" {
				t.Fatalf("expected gen_ai.provider.name=openai, got %v ok=%v", v, ok)
			}

			_, stateOk := attrValue(rootSpan, "ai.evaluation.state")
			if stateOk != gated {
				t.Errorf("ai.evaluation.state present=%v, want gated=%v", stateOk, gated)
			}

			var doEvaluateSpan sdktrace.ReadOnlySpan
			for _, s := range spans {
				if s.SpanContext().SpanID() != rootSpan.SpanContext().SpanID() {
					doEvaluateSpan = s
				}
			}
			if doEvaluateSpan == nil {
				t.Fatal("expected a distinct nested span")
			}
			// gen_ai.usage.* is core SemConv and always present, regardless of
			// the ExperimentalEvaluation gate.
			if v, ok := attrValue(doEvaluateSpan, "gen_ai.usage.input_tokens"); !ok || v.(int64) != 5 {
				t.Errorf("expected gen_ai.usage.input_tokens=5, got %v ok=%v", v, ok)
			}
			_, answersOk := attrValue(doEvaluateSpan, "ai.evaluation.answers")
			if answersOk != gated {
				t.Errorf("ai.evaluation.answers present=%v, want gated=%v", answersOk, gated)
			}
			_, metaOk := attrValue(doEvaluateSpan, "ai.response.providerMetadata")
			if !metaOk {
				t.Error("expected ai.response.providerMetadata (gated by ProviderMetadata option, not ExperimentalEvaluation)")
			}
		})
	}
}

// TestOpenTelemetryEvaluateSpans_ErrorClosesNestedSpan mirrors
// TestLegacyOpenTelemetryEvaluateSpans_ErrorClosesNestedSpan for the GenAI
// integration.
func TestOpenTelemetryEvaluateSpans_ErrorClosesNestedSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-evaluate-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnEvaluateStart(context.Background(), EvaluateStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.evaluate", ModelID: "gpt-5",
	})
	integration.OnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.evaluate.doEvaluate", ModelID: "gpt-5",
	})

	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom"), CallID: "call-err"})

	spans := findSpans(rec, "evaluate gpt-5")
	if len(spans) != 2 {
		t.Fatalf("expected 2 'evaluate gpt-5' spans, got %d", len(spans))
	}
	for _, s := range spans {
		if s.EndTime().IsZero() {
			t.Errorf("expected span %s to be ended", s.Name())
		}
	}
}
