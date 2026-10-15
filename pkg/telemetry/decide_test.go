package telemetry

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestLegacyOpenTelemetryDecideSpans mirrors TypeScript's
// onDecideOperationStart / experimental_onDecisionModelCallStart /
// experimental_onDecisionModelCallEnd / onDecideOperationEnd in
// packages/otel/src/legacy-open-telemetry.ts: the root "ai.decide" span
// carries input-gated ai.decision.state/questions and an output-gated
// ai.decision.answers, and the nested "ai.decide.doDecide" span
// additionally carries usage and providerMetadata (ungated). Ported from
// pkg/ai/telemetry_test.go's former TestExperimentalDecide_Telemetry,
// now exercising the LegacyOpenTelemetry integration directly instead of
// going through ExperimentalDecide.
func TestLegacyOpenTelemetryDecideSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("decide-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{
		IsEnabled:     Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "decide-test",
	}

	questions := map[string]interface{}{
		"q1": map[string]interface{}{"type": "boolean", "instructions": "Is it correct?"},
	}
	state := map[string]interface{}{"input": "hello"}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings:      settings,
		CallID:        "call-1",
		OperationID:   "ai.decide",
		ModelProvider: "test-provider",
		ModelID:       "test-model",
		State:         state,
		Questions:     questions,
		MaxRetries:    2,
	})

	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings:      settings,
		CallID:        "call-1",
		OperationID:   "ai.decide.doDecide",
		ModelProvider: "test-provider",
		ModelID:       "test-model",
		State:         state,
		Questions:     questions,
	})

	answers := map[string]interface{}{
		"q1": map[string]interface{}{"type": "boolean", "probability": 0.9},
	}
	inputTokens, outputTokens := int64(3), int64(7)
	integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{
			Settings:      settings,
			CallID:        "call-1",
			OperationID:   "ai.decide.doDecide",
			ModelProvider: "test-provider",
			ModelID:       "test-model",
			State:         state,
			Questions:     questions,
		},
		Answers:          answers,
		Usage:            TelemetryUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
		ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"key": "value"}},
	})

	integration.OnDecideEnd(ctx, DecideEndEvent{
		DecideStartEvent: DecideStartEvent{
			Settings:      settings,
			CallID:        "call-1",
			OperationID:   "ai.decide",
			ModelProvider: "test-provider",
			ModelID:       "test-model",
		},
		Answers: answers,
	})

	// The root span keeps its bare operation id as its real OTel name — TS
	// never suffixes the span name with functionID (see
	// legacyOperationNameAttrs); functionID surfaces only via the
	// operation.name/resource.name/ai.telemetry.functionId attributes.
	rootSpan := findSpan(rec, "ai.decide")
	if rootSpan == nil {
		t.Fatal("expected an 'ai.decide' root span")
	}
	doDecideSpan := findSpan(rec, "ai.decide.doDecide")
	if doDecideSpan == nil {
		t.Fatal("expected an 'ai.decide.doDecide' nested span")
	}

	if v, ok := attrValue(rootSpan, "ai.operationId"); !ok || v.(string) != "ai.decide" {
		t.Errorf("root ai.operationId = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "operation.name"); !ok || v.(string) != "ai.decide decide-test" {
		t.Errorf("root operation.name = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "resource.name"); !ok || v.(string) != "decide-test" {
		t.Errorf("root resource.name = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "ai.model.provider"); !ok || v.(string) != "test-provider" {
		t.Errorf("root ai.model.provider = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "ai.model.id"); !ok || v.(string) != "test-model" {
		t.Errorf("root ai.model.id = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "ai.settings.maxRetries"); !ok || v.(int64) != 2 {
		t.Errorf("root ai.settings.maxRetries = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(rootSpan, "ai.decision.state"); !ok || v.(string) != `{"input":"hello"}` {
		t.Errorf("root ai.decision.state = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(rootSpan, "ai.decision.questions"); !ok {
		t.Error("root span missing ai.decision.questions")
	}
	if _, ok := attrValue(rootSpan, "ai.decision.answers"); !ok {
		t.Error("root span missing ai.decision.answers")
	}
	// TS's onDecideOperationStart carries no gen_ai.* attributes at all
	// (legacy-open-telemetry.ts); follow-up H1 removed the Go-only
	// gen_ai.system/gen_ai.request.model that used to be set here.
	if _, ok := attrValue(rootSpan, "gen_ai.system"); ok {
		t.Error("root span should not carry gen_ai.system (TS parity)")
	}
	if _, ok := attrValue(rootSpan, "gen_ai.request.model"); ok {
		t.Error("root span should not carry gen_ai.request.model (TS parity)")
	}

	if v, ok := attrValue(doDecideSpan, "ai.operationId"); !ok || v.(string) != "ai.decide.doDecide" {
		t.Errorf("doDecide ai.operationId = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(doDecideSpan, "ai.decision.state"); !ok {
		t.Error("doDecide span missing ai.decision.state")
	}
	if _, ok := attrValue(doDecideSpan, "ai.decision.answers"); !ok {
		t.Error("doDecide span missing ai.decision.answers")
	}
	if v, ok := attrValue(doDecideSpan, "ai.usage.inputTokens"); !ok || v.(int64) != 3 {
		t.Errorf("doDecide ai.usage.inputTokens = %v, ok=%v", v, ok)
	}
	if v, ok := attrValue(doDecideSpan, "ai.usage.outputTokens"); !ok || v.(int64) != 7 {
		t.Errorf("doDecide ai.usage.outputTokens = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(doDecideSpan, "ai.response.providerMetadata"); !ok {
		t.Error("doDecide span missing ai.response.providerMetadata")
	}

	// Both spans must belong to the same trace (doDecide is a child of the
	// root, mirroring the nested-span relationship in decide.ts).
	if rootSpan.SpanContext().TraceID() != doDecideSpan.SpanContext().TraceID() {
		t.Error("expected root and doDecide spans to share a trace ID")
	}
}

// TestLegacyOpenTelemetryDecideSpans_RecordInputsFalse verifies
// ai.decision.state/questions are omitted from both spans when
// RecordInputs is false, matching selectAttributes()'s input gating.
func TestLegacyOpenTelemetryDecideSpans_RecordInputsFalse(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("decide-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: false, RecordOutputs: true}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings:    settings,
		CallID:      "call-1",
		OperationID: "ai.decide",
		State:       "secret state",
		Questions:   map[string]interface{}{"q1": "secret question"},
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings:    settings,
		CallID:      "call-1",
		OperationID: "ai.decide.doDecide",
		State:       "secret state",
		Questions:   map[string]interface{}{"q1": "secret question"},
	})
	integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide.doDecide"},
		Answers:                     map[string]interface{}{"q1": "answer"},
	})
	integration.OnDecideEnd(ctx, DecideEndEvent{
		DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide"},
		Answers:          map[string]interface{}{"q1": "answer"},
	})

	rootSpan := findSpan(rec, "ai.decide")
	doDecideSpan := findSpan(rec, "ai.decide.doDecide")
	if rootSpan == nil || doDecideSpan == nil {
		t.Fatal("expected both root and nested spans")
	}
	for _, s := range []sdktrace.ReadOnlySpan{rootSpan, doDecideSpan} {
		if _, ok := attrValue(s, "ai.decision.state"); ok {
			t.Errorf("span %s: expected ai.decision.state to be omitted when RecordInputs is false", s.Name())
		}
		if _, ok := attrValue(s, "ai.decision.questions"); ok {
			t.Errorf("span %s: expected ai.decision.questions to be omitted when RecordInputs is false", s.Name())
		}
	}
	// Answers (output) should still be recorded since RecordOutputs is true.
	if _, ok := attrValue(rootSpan, "ai.decision.answers"); !ok {
		t.Error("root span missing ai.decision.answers")
	}
}

// TestLegacyOpenTelemetryDecideSpans_RecordOutputsFalse verifies
// ai.decision.answers is omitted when RecordOutputs is false, while usage
// and providerMetadata (ungated) remain on the nested span.
func TestLegacyOpenTelemetryDecideSpans_RecordOutputsFalse(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("decide-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: false}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "call-1", OperationID: "ai.decide",
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings: settings, CallID: "call-1", OperationID: "ai.decide.doDecide",
	})
	inputTokens := int64(1)
	integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide.doDecide"},
		Answers:                     map[string]interface{}{"q1": "answer"},
		Usage:                       TelemetryUsage{InputTokens: &inputTokens},
		ProviderMetadata:            map[string]interface{}{"p": "v"},
	})
	integration.OnDecideEnd(ctx, DecideEndEvent{
		DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide"},
		Answers:          map[string]interface{}{"q1": "answer"},
	})

	rootSpan := findSpan(rec, "ai.decide")
	doDecideSpan := findSpan(rec, "ai.decide.doDecide")
	if rootSpan == nil || doDecideSpan == nil {
		t.Fatal("expected both root and nested spans")
	}
	if _, ok := attrValue(rootSpan, "ai.decision.answers"); ok {
		t.Error("root span: expected ai.decision.answers to be omitted when RecordOutputs is false")
	}
	if _, ok := attrValue(doDecideSpan, "ai.decision.answers"); ok {
		t.Error("doDecide span: expected ai.decision.answers to be omitted when RecordOutputs is false")
	}
	// usage and providerMetadata are unconditional (not output-gated).
	if v, ok := attrValue(doDecideSpan, "ai.usage.inputTokens"); !ok || v.(int64) != 1 {
		t.Errorf("doDecide ai.usage.inputTokens = %v, ok=%v", v, ok)
	}
	if _, ok := attrValue(doDecideSpan, "ai.response.providerMetadata"); !ok {
		t.Error("doDecide span missing ai.response.providerMetadata")
	}
}

// TestLegacyOpenTelemetryDecideSpans_ErrorClosesNestedSpan verifies the
// defensive behavior called out in the follow-up: since
// experimental_onDecisionModelCallEnd is never notified on an error path
// (mirrors decide.ts), OnError must find and close the nested
// "ai.decide.doDecide" span via CallID so it never leaks, in addition to
// closing the root span.
func TestLegacyOpenTelemetryDecideSpans_ErrorClosesNestedSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("decide-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.decide",
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.decide.doDecide",
	})

	// No OnDecisionModelCallEnd notified — go straight to OnError, as
	// decide.go does on a failed DoDecide call or answer validation error.
	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom"), CallID: "call-err"})

	rootSpan := findSpan(rec, "ai.decide")
	doDecideSpan := findSpan(rec, "ai.decide.doDecide")
	if rootSpan == nil {
		t.Fatal("expected root span")
	}
	if doDecideSpan == nil {
		t.Fatal("expected nested doDecide span to have been recorded before being closed")
	}
	if rootSpan.EndTime().IsZero() {
		t.Error("expected root span to be ended by OnError")
	}
	if doDecideSpan.EndTime().IsZero() {
		t.Error("expected nested doDecide span to be defensively closed by OnError")
	}

	// The span must not be double-closeable / re-found via the same CallID
	// (LoadAndDelete semantics) — a second OnError call must not panic.
	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom again"), CallID: "call-err"})
}

// TestOpenTelemetryDecideSpans_ExperimentalDecisionGate verifies the
// GenAI OpenTelemetry integration emits gen_ai.* attributes unconditionally
// but gates ai.decision.state/questions/answers behind
// OpenTelemetryOptions.ExperimentalDecision, matching TS's
// selectSupplementalAttributes(telemetry, this.supplementalAttributes, {
// experimental_decision: {...} }) in otel/src/open-telemetry.ts. P1-7b
// left this option inert because decide didn't exist yet; this closes
// that gap.
func TestOpenTelemetryDecideSpans_ExperimentalDecisionGate(t *testing.T) {
	for _, gated := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[gated], func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			tracer := tp.Tracer("genai-decide-test")

			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer, ExperimentalDecision: gated, ProviderMetadata: true})
			settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

			ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
				Settings:      settings,
				CallID:        "call-1",
				OperationID:   "ai.decide",
				ModelProvider: "openai.chat",
				ModelID:       "gpt-5",
				State:         "hello",
				Questions:     map[string]interface{}{"q1": "is it correct?"},
			})
			integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
				Settings:      settings,
				CallID:        "call-1",
				OperationID:   "ai.decide.doDecide",
				ModelProvider: "openai.chat",
				ModelID:       "gpt-5",
				State:         "hello",
				Questions:     map[string]interface{}{"q1": "is it correct?"},
			})
			inputTokens := int64(5)
			integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
				DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide.doDecide"},
				Answers:                     map[string]interface{}{"q1": "yes"},
				Usage:                       TelemetryUsage{InputTokens: &inputTokens},
				ProviderMetadata:            map[string]interface{}{"p": "v"},
			})
			integration.OnDecideEnd(ctx, DecideEndEvent{
				DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide"},
				Answers:          map[string]interface{}{"q1": "yes"},
			})

			rootSpan := findSpan(rec, "decide gpt-5")
			spans := findSpans(rec, "decide gpt-5")
			if len(spans) != 2 {
				t.Fatalf("expected 2 'decide gpt-5' spans (root + nested), got %d", len(spans))
			}
			if rootSpan == nil {
				t.Fatal("expected a root 'decide gpt-5' span")
			}
			if v, ok := attrValue(rootSpan, "gen_ai.operation.name"); !ok || v.(string) != "decide" {
				t.Fatalf("expected gen_ai.operation.name=decide, got %v ok=%v", v, ok)
			}
			if v, ok := attrValue(rootSpan, "gen_ai.provider.name"); !ok || v.(string) != "openai" {
				t.Fatalf("expected gen_ai.provider.name=openai, got %v ok=%v", v, ok)
			}

			_, stateOk := attrValue(rootSpan, "ai.decision.state")
			if stateOk != gated {
				t.Errorf("ai.decision.state present=%v, want gated=%v", stateOk, gated)
			}

			var doDecideSpan sdktrace.ReadOnlySpan
			for _, s := range spans {
				if s.SpanContext().SpanID() != rootSpan.SpanContext().SpanID() {
					doDecideSpan = s
				}
			}
			if doDecideSpan == nil {
				t.Fatal("expected a distinct nested span")
			}
			// gen_ai.usage.* is core SemConv and always present, regardless of
			// the ExperimentalDecision gate.
			if v, ok := attrValue(doDecideSpan, "gen_ai.usage.input_tokens"); !ok || v.(int64) != 5 {
				t.Errorf("expected gen_ai.usage.input_tokens=5, got %v ok=%v", v, ok)
			}
			_, answersOk := attrValue(doDecideSpan, "ai.decision.answers")
			if answersOk != gated {
				t.Errorf("ai.decision.answers present=%v, want gated=%v", answersOk, gated)
			}
			_, metaOk := attrValue(doDecideSpan, "ai.response.providerMetadata")
			if !metaOk {
				t.Error("expected ai.response.providerMetadata (gated by ProviderMetadata option, not ExperimentalDecision)")
			}
		})
	}
}

// TestOpenTelemetryDecideSpans_ProviderMetadataGatedByOptionOnly covers
// follow-up H2 for decide.go specifically: ai.response.providerMetadata on
// the nested "ai.decide.doDecide"-equivalent span must appear whenever
// OpenTelemetryOptions.ProviderMetadata is set, regardless of
// Settings.RecordOutputs (TS gates it only on the providerMetadata
// supplemental flag, since it is a plain pre-computed value, not an
// {output: () => ...} accessor).
func TestOpenTelemetryDecideSpans_ProviderMetadataGatedByOptionOnly(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-decide-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer, ProviderMetadata: true})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: false}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "call-pm", OperationID: "ai.decide", ModelID: "gpt-5",
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings: settings, CallID: "call-pm", OperationID: "ai.decide.doDecide", ModelID: "gpt-5",
	})
	integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: "call-pm", OperationID: "ai.decide.doDecide"},
		Answers:                     map[string]interface{}{"q1": "yes"},
		ProviderMetadata:            map[string]interface{}{"p": "v"},
	})

	spans := findSpans(rec, "decide gpt-5")
	if len(spans) != 2 {
		t.Fatalf("expected 2 'decide gpt-5' spans, got %d", len(spans))
	}
	var found bool
	for _, s := range spans {
		if _, ok := attrValue(s, "ai.response.providerMetadata"); ok {
			found = true
		}
	}
	if !found {
		t.Error("expected ai.response.providerMetadata on the nested decide span even with RecordOutputs=false")
	}
}

// TestOpenTelemetryDecideSpans_ErrorClosesNestedSpan mirrors
// TestLegacyOpenTelemetryDecideSpans_ErrorClosesNestedSpan for the GenAI
// integration.
func TestOpenTelemetryDecideSpans_ErrorClosesNestedSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-decide-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.decide", ModelID: "gpt-5",
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings: settings, CallID: "call-err", OperationID: "ai.decide.doDecide", ModelID: "gpt-5",
	})

	integration.OnError(ctx, TelemetryErrorEvent{Settings: settings, Error: errors.New("boom"), CallID: "call-err"})

	spans := findSpans(rec, "decide gpt-5")
	if len(spans) != 2 {
		t.Fatalf("expected 2 'decide gpt-5' spans, got %d", len(spans))
	}
	for _, s := range spans {
		if s.EndTime().IsZero() {
			t.Errorf("expected span %s to be ended", s.Name())
		}
	}
}

// TS (#22542): the nested model-call span records the resolved model and
// response ID.
func TestOpenTelemetryDecideSpans_RecordsResponseIDAndModel(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-decide-test")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "call-1", OperationID: "ai.decide", ModelProvider: "openai.chat", ModelID: "gpt-5",
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings: settings, CallID: "call-1", OperationID: "ai.decide.doDecide", ModelProvider: "openai.chat", ModelID: "gpt-5",
	})
	integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide.doDecide"},
		ResponseID:                  "test-response-id",
		ResponseModelID:             "mock-model-resolved",
	})
	integration.OnDecideEnd(ctx, DecideEndEvent{DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "call-1", OperationID: "ai.decide"}})

	root := findSpan(rec, "decide gpt-5")
	for _, s := range findSpans(rec, "decide gpt-5") {
		if s.SpanContext().SpanID() == root.SpanContext().SpanID() {
			continue
		}
		if v, ok := attrValue(s, "gen_ai.response.id"); !ok || v.(string) != "test-response-id" {
			t.Errorf("gen_ai.response.id = %v ok=%v", v, ok)
		}
		if v, ok := attrValue(s, "gen_ai.response.model"); !ok || v.(string) != "mock-model-resolved" {
			t.Errorf("gen_ai.response.model = %v ok=%v", v, ok)
		}
		return
	}
	t.Fatal("nested model-call span not found")
}

// TS (#21935): experimental_evaluation remains a deprecated alias of
// experimental_decision.
func TestOpenTelemetryDecideSpans_DeprecatedExperimentalEvaluationOption(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t"), ExperimentalEvaluation: true})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}
	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "c", OperationID: "ai.decide", ModelProvider: "openai", ModelID: "m", State: "hello",
	})
	integration.OnDecideEnd(ctx, DecideEndEvent{DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "c", OperationID: "ai.decide"}})
	root := findSpan(rec, "decide m")
	if _, ok := attrValue(root, "ai.decision.state"); !ok {
		t.Fatal("expected ai.decision.state with the deprecated ExperimentalEvaluation option")
	}
}

// TS experimental_onDecisionModelCallStart spreads the root's
// baseSupplementalAttributes (headers) and calls enrichSpan with spanType
// experimental_decision.
func TestOpenTelemetryDecideSpans_ChildCarriesBaseSupplementalAndEnrich(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	var gotType SpanType
	integration := NewOpenTelemetry(OpenTelemetryOptions{
		Tracer:  tp.Tracer("genai-decide-test"),
		Headers: true,
		EnrichSpan: func(_ context.Context, o EnrichSpanOptions) map[string]interface{} {
			if o.SpanType == SpanTypeDecision {
				gotType = o.SpanType
			}
			return nil
		},
	})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}
	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "call-b", OperationID: "ai.decide", ModelProvider: "openai.chat", ModelID: "gpt-5",
		Headers: map[string]string{"x-a": "1"},
	})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{
		Settings: settings, CallID: "call-b", OperationID: "ai.decide.doDecide", ModelProvider: "openai.chat", ModelID: "gpt-5",
	})
	integration.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: "call-b"},
	})
	integration.OnDecideEnd(ctx, DecideEndEvent{DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "call-b"}})

	if gotType != SpanTypeDecision {
		t.Fatalf("enrichSpan never saw %q", SpanTypeDecision)
	}
	spans := findSpans(rec, "decide gpt-5")
	if len(spans) != 2 {
		t.Fatalf("spans = %d", len(spans))
	}
	for _, s := range spans {
		if v, ok := attrValue(s, "ai.request.headers.x-a"); !ok || v.(string) != "1" {
			t.Errorf("span %s missing header attr", s.Name())
		}
	}
}
