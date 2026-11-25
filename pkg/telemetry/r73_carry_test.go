package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r73carryRerank runs one rerank call through integration and returns the
// recorded spans.
func r73carryRerank(t *testing.T, newIntegration func(tracer *sdktrace.TracerProvider) TelemetryIntegration) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := newIntegration(tp)
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(true), RecordOutputs: Bool(true)}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		CallID: "r73-rerank", OperationType: "ai.rerank", ModelProvider: "mock-provider", ModelID: "mock-model-id",
		Settings: settings, Documents: []string{"a", "b", "c"},
	})
	integration.(rerankStartHandler).OnRerankStart(ctx, RerankingModelCallStartEvent{
		Settings: settings, CallID: "r73-rerank", OperationID: "ai.rerank.doRerank",
		ModelProvider: "mock-provider", ModelID: "mock-model-id", Documents: []string{"a", "b", "c"},
	})
	integration.(rerankEndHandler).OnRerankEnd(ctx, RerankingModelCallEndEvent{
		Settings: settings, CallID: "r73-rerank", OperationID: "ai.rerank.doRerank",
		ModelProvider: "mock-provider", ModelID: "mock-model-id", DocumentsType: "text",
		Ranking: []types.RerankItem{{Index: 2, RelevanceScore: 0.9}, {Index: 0, RelevanceScore: 0.8}},
	})
	integration.(endHandler).OnEnd(ctx, TelemetryFinishEvent{CallID: "r73-rerank", OperationType: "ai.rerank", Settings: settings})
	return rec
}

// TS legacy-open-telemetry.test.ts rerank snapshot: ai.ranking entries are
// `{"index":2,"relevanceScore":0.9}` (JSON.stringify of the ranking entry).
func TestR73Carry_RankingAttributeUsesTSKeys(t *testing.T) {
	want := []string{`{"index":2,"relevanceScore":0.9}`, `{"index":0,"relevanceScore":0.8}`}
	cases := map[string]func(tp *sdktrace.TracerProvider) TelemetryIntegration{
		"legacy": func(tp *sdktrace.TracerProvider) TelemetryIntegration {
			return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		},
		"genai": func(tp *sdktrace.TracerProvider) TelemetryIntegration {
			return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t"), Reranking: true})
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			rec := r73carryRerank(t, mk)
			var got []string
			for _, s := range rec.Ended() {
				for _, kv := range s.Attributes() {
					if string(kv.Key) == "ai.ranking" {
						got = kv.Value.AsStringSlice()
					}
				}
			}
			if len(got) != len(want) {
				t.Fatalf("ai.ranking = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("ai.ranking[%d] = %s, want %s", i, got[i], want[i])
				}
			}
		})
	}
}
