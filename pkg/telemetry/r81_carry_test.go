package telemetry

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// r81carryCloseCase drives a root call with two open embedding spans and one
// open rerank span that never get OnEmbedEnd/OnRerankEnd, then ends the root
// with OnError or OnAbort (reviewer item 2, r81).
type r81carryCloseCase struct {
	name  string
	abort bool
}

var r81carryCloseCases = []r81carryCloseCase{{"error", false}, {"abort", true}}

func TestR81carryOpenTelemetryCloseDropsEmbedAndRerankEntries(t *testing.T) {
	for _, tc := range r81carryCloseCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			it := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
			settings := &Settings{IsEnabled: Bool(true)}
			callID := "r81carry-genai-" + tc.name
			ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.embedMany", Settings: settings})
			for _, id := range []string{"e1", "e2"} {
				it.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{Settings: settings, CallID: callID, EmbedCallID: id, OperationID: "ai.embedMany.doEmbed", ModelID: "m"})
			}
			it.OnRerankStart(ctx, RerankingModelCallStartEvent{Settings: settings, CallID: callID, OperationID: "ai.rerank.doRerank", ModelID: "m"})
			keys := []string{
				genAISpanKey("embedding", it.stateKey("e1")),
				genAISpanKey("embedding", it.stateKey("e2")),
				genAISpanKey("reranking", it.stateKey(callID)),
			}
			for _, k := range keys {
				if _, ok := genAICallSpans.Load(k); !ok {
					t.Fatalf("precondition: %s not stored", k)
				}
			}
			if tc.abort {
				it.OnAbort(ctx, TelemetryAbortEvent{CallID: callID})
			} else {
				it.OnError(ctx, TelemetryErrorEvent{Settings: settings, CallID: callID, Error: errors.New("boom")})
			}
			for _, k := range keys {
				if _, ok := genAICallSpans.Load(k); ok {
					t.Errorf("%s left in genAICallSpans after %s", k, tc.name)
				}
			}
			if got := len(rec.Ended()); got != 4 {
				t.Errorf("ended spans = %d, want 4 (root, 2 embeddings, rerank)", got)
			}
		})
	}
}

func TestR81carryLegacyOpenTelemetryCloseDropsEmbedAndRerankEntries(t *testing.T) {
	for _, tc := range r81carryCloseCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			it := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
			settings := &Settings{IsEnabled: Bool(true)}
			callID := "r81carry-legacy-" + tc.name
			ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.embedMany", Settings: settings})
			for _, id := range []string{"e1", "e2"} {
				it.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{Settings: settings, CallID: callID, EmbedCallID: id, OperationID: "ai.embedMany.doEmbed"})
			}
			it.OnRerankStart(ctx, RerankingModelCallStartEvent{Settings: settings, CallID: callID, OperationID: "ai.rerank.doRerank"})
			keys := []string{
				otelSpanKey("embedding", it.stateKey("e1")),
				otelSpanKey("embedding", it.stateKey("e2")),
				otelSpanKey("reranking", it.stateKey(callID)),
			}
			for _, k := range keys {
				if _, ok := otelModelCallSpans.Load(k); !ok {
					t.Fatalf("precondition: %s not stored", k)
				}
			}
			if tc.abort {
				it.OnAbort(ctx, TelemetryAbortEvent{CallID: callID})
			} else {
				it.OnError(ctx, TelemetryErrorEvent{Settings: settings, CallID: callID, Error: errors.New("boom")})
			}
			for _, k := range keys {
				if _, ok := otelModelCallSpans.Load(k); ok {
					t.Errorf("%s left in otelModelCallSpans after %s", k, tc.name)
				}
			}
			if got := len(rec.Ended()); got != 4 {
				t.Errorf("ended spans = %d, want 4 (root, 2 embeddings, rerank)", got)
			}
		})
	}
}
