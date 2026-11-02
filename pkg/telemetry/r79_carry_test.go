package telemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// r79carryIntegration is the subset of both OpenTelemetry integrations the
// span end order tests drive.
type r79carryIntegration interface {
	OnStart(context.Context, TelemetryStartEvent) context.Context
	OnStepStart(context.Context, TelemetryStepStartEvent) context.Context
	OnToolExecutionStart(context.Context, TelemetryToolCallStartEvent) context.Context
	OnEmbedStart(context.Context, EmbeddingModelCallStartEvent)
	OnError(context.Context, TelemetryErrorEvent)
	OnAbort(context.Context, TelemetryAbortEvent)
}

const r79carryToolCount = 8

// r79carryStartTextCall opens a generateText root, a step, a chat span (GenAI
// only) and r79carryToolCount tool spans, returning the span IDs in the order
// TS onAbort/onError end them: tool spans in start order, the chat span, the
// step span, the root.
func r79carryStartTextCall(t *testing.T, rec *tracetest.SpanRecorder, it r79carryIntegration, callID string) []trace.SpanID {
	t.Helper()
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateText", ModelID: "m", Settings: settings})
	stepCtx := it.OnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, OperationType: "ai.generateText", ModelID: "m", Settings: settings})
	if genAI, ok := it.(OpenTelemetry); ok {
		genAI.OnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{CallID: callID, ModelID: "m", Settings: settings})
	}
	// Tool call IDs in reverse lexical order, so neither a map nor a sort
	// yields start order by accident.
	for n := range r79carryToolCount {
		it.OnToolExecutionStart(stepCtx, TelemetryToolCallStartEvent{
			CallID: callID, ToolCallID: fmt.Sprintf("call-%d", r79carryToolCount-n), ToolName: fmt.Sprintf("tool%d", n), Settings: settings,
		})
	}
	started := rec.Started()
	var root, step, chat trace.SpanID
	var tools []trace.SpanID
	for _, s := range started {
		switch name := s.Name(); {
		case name == "invoke_agent m" || name == "ai.generateText":
			root = s.SpanContext().SpanID()
		case name == "step 1" || name == "ai.generateText.doGenerate":
			step = s.SpanContext().SpanID()
		case name == "chat m":
			chat = s.SpanContext().SpanID()
		default:
			tools = append(tools, s.SpanContext().SpanID())
		}
	}
	if len(tools) != r79carryToolCount {
		t.Fatalf("started %d tool spans, want %d", len(tools), r79carryToolCount)
	}
	want := append([]trace.SpanID{}, tools...)
	if chat.IsValid() {
		want = append(want, chat)
	}
	return append(want, step, root)
}

// r79carryStartEmbedCall opens an embedMany root with r79carryToolCount
// embedding spans, returning their IDs in start order followed by the root.
func r79carryStartEmbedCall(t *testing.T, rec *tracetest.SpanRecorder, it r79carryIntegration, callID string) []trace.SpanID {
	t.Helper()
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.embedMany", ModelID: "m", Settings: settings})
	for n := range r79carryToolCount {
		it.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{
			CallID: callID, EmbedCallID: fmt.Sprintf("embed-%d", r79carryToolCount-n), OperationID: "ai.embedMany.doEmbed", ModelID: "m", Settings: settings, Values: []string{"v"},
		})
	}
	started := rec.Started()
	want := make([]trace.SpanID, 0, len(started))
	for _, s := range started[1:] {
		want = append(want, s.SpanContext().SpanID())
	}
	return append(want, started[0].SpanContext().SpanID())
}

func r79carryAssertEndOrder(t *testing.T, rec *tracetest.SpanRecorder, want []trace.SpanID) {
	t.Helper()
	ended := rec.Ended()
	if len(ended) != len(want) {
		t.Fatalf("ended %d spans, want %d", len(ended), len(want))
	}
	for n, s := range ended {
		if s.SpanContext().SpanID() != want[n] {
			var names []string
			for _, e := range ended {
				names = append(names, e.Name())
			}
			t.Fatalf("span %d ended out of TS order; ended: %q", n, names)
		}
	}
}

// Round 79 carry: TS onAbort/onError end a call's open child spans in a fixed
// order, the Map-held tool and embedding spans in insertion order (GenAI:
// tools, chat, step, embeddings, rerank, decision, root; legacy: tools, step,
// embeddings, rerank, decision, root). Go ended the step span before the tool
// spans inside it, and iterated Go maps, so parallel tool or embedding spans
// ended in random order.
func TestR79Carry_AbortAndErrorEndOpenSpansInTSOrder(t *testing.T) {
	integrations := map[string]func(trace.Tracer) r79carryIntegration{
		"genai": func(tr trace.Tracer) r79carryIntegration { return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tr}) },
		"legacy": func(tr trace.Tracer) r79carryIntegration {
			return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tr})
		},
	}
	for name, build := range integrations {
		for _, terminal := range []string{"abort", "error"} {
			t.Run(name+"/"+terminal+"/text", func(t *testing.T) {
				rec := tracetest.NewSpanRecorder()
				it := build(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry"))
				callID := "r79carry-text-" + name + "-" + terminal
				want := r79carryStartTextCall(t, rec, it, callID)
				if terminal == "abort" {
					it.OnAbort(context.Background(), TelemetryAbortEvent{CallID: callID})
				} else {
					it.OnError(context.Background(), TelemetryErrorEvent{CallID: callID, Error: errors.New("boom")})
				}
				r79carryAssertEndOrder(t, rec, want)
			})
			t.Run(name+"/"+terminal+"/embedMany", func(t *testing.T) {
				rec := tracetest.NewSpanRecorder()
				it := build(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry"))
				callID := "r79carry-embed-" + name + "-" + terminal
				want := r79carryStartEmbedCall(t, rec, it, callID)
				if terminal == "abort" {
					it.OnAbort(context.Background(), TelemetryAbortEvent{CallID: callID})
				} else {
					it.OnError(context.Background(), TelemetryErrorEvent{CallID: callID, Error: errors.New("boom")})
				}
				r79carryAssertEndOrder(t, rec, want)
			})
		}
	}
}

// spanSet keeps Map semantics: re-setting a key keeps its position, take
// removes it, drain empties the set.
func TestR79Carry_SpanSetKeepsInsertionOrder(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tr := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry")
	span := func(name string) trace.Span {
		_, s := tr.Start(context.Background(), name)
		return s
	}
	var s spanSet
	a, b, c, a2 := span("a"), span("b"), span("c"), span("a2")
	s.set("a", a)
	s.set("b", b)
	s.set("c", c)
	s.set("a", a2)
	if got := s.take("b"); got != b {
		t.Fatalf("take(b) = %v, want b", got)
	}
	if got := s.take("missing"); got != nil {
		t.Fatalf("take(missing) = %v, want nil", got)
	}
	got := s.drain()
	if len(got) != 2 || got[0] != a2 || got[1] != c {
		t.Fatalf("drain = %v, want [a2 c]", got)
	}
	if rest := s.drain(); len(rest) != 0 {
		t.Fatalf("second drain = %v, want empty", rest)
	}
}
