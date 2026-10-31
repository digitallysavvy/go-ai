package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r74carrySpanAttr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit(), true
		}
	}
	return "", false
}

// TS open-telemetry.ts onEmbedStart: the embeddings model-call span is
// `gen_ai.operation.name: 'embeddings'` for embed and embedMany alike; Go's
// events carry the call operation ID ("ai.embed.doEmbed",
// "ai.embedMany.doEmbed"), which must not leak into the attribute.
func TestR74Carry_GenAIEmbeddingSpanOperationName(t *testing.T) {
	for _, op := range []string{"ai.embed", "ai.embedMany"} {
		t.Run(op, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
			settings := &Settings{IsEnabled: Bool(true)}
			ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
				CallID: "r74-embed", OperationType: op, ModelProvider: "mock-provider", ModelID: "mock-model-id", Settings: settings,
			})
			integration.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{
				Settings: settings, CallID: "r74-embed", EmbedCallID: "e1", OperationID: op + ".doEmbed",
				ModelProvider: "mock-provider", ModelID: "mock-model-id", Values: []string{"a"},
			})
			integration.OnEmbedEnd(ctx, EmbeddingModelCallEndEvent{
				Settings: settings, CallID: "r74-embed", EmbedCallID: "e1", OperationID: op + ".doEmbed",
				ModelProvider: "mock-provider", ModelID: "mock-model-id", Usage: types.EmbeddingUsage{InputTokens: 3},
			})
			integration.OnEnd(ctx, TelemetryFinishEvent{CallID: "r74-embed", OperationType: op, Settings: settings})
			found := false
			for _, s := range rec.Ended() {
				if !s.Parent().SpanID().IsValid() {
					continue
				}
				found = true
				if got, _ := r74carrySpanAttr(s, "gen_ai.operation.name"); got != "embeddings" {
					t.Errorf("embeddings span gen_ai.operation.name = %q, want embeddings", got)
				}
			}
			if !found {
				t.Fatal("no embeddings child span recorded")
			}
		})
	}
}
