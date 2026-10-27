package telemetry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TS reads call state with getCallState (this.callStates.get), which never
// creates it, and every handler but the operation starts returns when the
// callId has no state. Go's root-span and supplemental-attribute lookups used
// the creating lookup and then fell back to the span in ctx, so an event for
// a call this integration never saw start created state only that call's end
// event would delete, and could nest spans under (or end) the caller's span.
func TestUnseenCallEventsLeaveNoStateAndCtxSpanAlone(t *testing.T) {
	type full interface {
		TelemetryIntegration
		OnAbort(context.Context, TelemetryAbortEvent)
		OnStepError(context.Context, TelemetryErrorEvent)
		OnEnd(context.Context, TelemetryFinishEvent)
		OnEmbedStart(context.Context, EmbeddingModelCallStartEvent)
		OnEmbedEnd(context.Context, EmbeddingModelCallEndEvent)
		OnRerankStart(context.Context, RerankingModelCallStartEvent)
		OnRerankEnd(context.Context, RerankingModelCallEndEvent)
		OnDecideEnd(context.Context, DecideEndEvent)
		OnDecisionModelCallStart(context.Context, DecisionModelCallStartEvent)
		OnDecisionModelCallEnd(context.Context, DecisionModelCallEndEvent)
	}
	for _, tc := range []struct {
		name string
		make func(trace.Tracer) full
	}{
		{"legacy", func(tr trace.Tracer) full { return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tr}) }},
		{"genai", func(tr trace.Tracer) full { return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tr}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			in, other := tc.make(tp.Tracer("ai")), tc.make(tp.Tracer("ai"))
			ctx, caller := tp.Tracer("caller").Start(context.Background(), "caller span")
			defer caller.End()
			// A second instance of the same integration has an open root and
			// step span in ctx; the events below are not its call.
			otherID := "call-other-r47-" + tc.name
			ctx = other.OnStart(ctx, TelemetryStartEvent{CallID: otherID, OperationType: "ai.generateText", ModelID: "m"})
			ctx = other.OnStepStart(ctx, TelemetryStepStartEvent{CallID: otherID, OperationType: "ai.generateText"})
			before := len(rec.Started())
			defer other.OnEnd(ctx, TelemetryFinishEvent{CallID: otherID})

			id := "call-unseen-r47-" + tc.name
			var settings *Settings // nil: enabled
			sctx := in.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, CallID: id})
			if lm, ok := in.(interface {
				OnLanguageModelCallStart(context.Context, LanguageModelCallStartEvent) context.Context
				OnLanguageModelCallEnd(context.Context, LanguageModelCallEndEvent)
			}); ok {
				lctx := lm.OnLanguageModelCallStart(sctx, LanguageModelCallStartEvent{Settings: settings, CallID: id, ModelID: "m"})
				lm.OnLanguageModelCallEnd(lctx, LanguageModelCallEndEvent{Settings: settings, CallID: id})
			}
			in.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{Settings: settings, CallID: id, EmbedCallID: "e1"})
			in.OnEmbedEnd(ctx, EmbeddingModelCallEndEvent{Settings: settings, CallID: id, EmbedCallID: "e1"})
			in.OnRerankStart(ctx, RerankingModelCallStartEvent{Settings: settings, CallID: id})
			in.OnRerankEnd(ctx, RerankingModelCallEndEvent{Settings: settings, CallID: id})
			in.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{Settings: settings, CallID: id})
			in.OnDecisionModelCallEnd(ctx, DecisionModelCallEndEvent{DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings, CallID: id}})
			in.OnStepEnd(sctx, TelemetryStepEndEvent{Settings: settings, CallID: id})
			in.OnStepError(sctx, TelemetryErrorEvent{Settings: settings, CallID: id, Error: errors.New("boom")})
			in.OnDecideEnd(ctx, DecideEndEvent{DecideStartEvent: DecideStartEvent{Settings: settings, CallID: id}})
			in.OnError(ctx, TelemetryErrorEvent{Settings: settings, CallID: id, Error: errors.New("boom")})
			in.OnAbort(ctx, TelemetryAbortEvent{Settings: settings, CallID: id})
			in.OnEnd(ctx, TelemetryFinishEvent{Settings: settings, CallID: id})

			var names []string
			for _, s := range rec.Started()[before:] {
				names = append(names, s.Name())
			}
			if len(names) != 0 {
				t.Errorf("started spans %q for a call this integration never saw", names)
			}
			if n := len(rec.Ended()); n != 0 {
				t.Errorf("the integration ended %d span(s) it does not own", n)
			}
			for name, states := range map[string]*sync.Map{"GenAI": &genAICallStates, "legacy": &legacyCallStates} {
				states.Range(func(key, _ interface{}) bool {
					if strings.HasSuffix(key.(string), id) {
						// only the unseen id; otherID's state is the other instance's
						t.Errorf("events created %s call state %q", name, key)
					}
					return true
				})
			}
		})
	}
}

// The readers alone must not create state: TS getCallState.
func TestRootSpanAndSupplementalLookupsDoNotCreateState(t *testing.T) {
	const id = "call-reader-r47"
	_ = genAIRootSpanFor(id, context.Background())
	_ = genAIBaseSupplemental(id)
	_ = legacyRootSpanFor(id, context.Background())
	if _, ok := genAICallStates.Load(id); ok {
		t.Error("genAIRootSpanFor/genAIBaseSupplemental created GenAI call state")
	}
	if _, ok := legacyCallStates.Load(id); ok {
		t.Error("legacyRootSpanFor created legacy call state")
	}
}
