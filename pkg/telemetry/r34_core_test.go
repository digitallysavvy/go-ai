package telemetry

import (
	"context"
	"math"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS onEmbedEnd records gen_ai.usage.input_tokens from usage.tokens; a
// provider that fills only the TS-shaped Tokens field must still be counted,
// and a NaN is dropped.
func TestOpenTelemetryEmbedUsageReadsTokensField(t *testing.T) {
	for _, tc := range []struct {
		name   string
		usage  types.EmbeddingUsage
		want   int64
		wantOK bool
	}{
		{"tokens only", types.EmbeddingUsage{Tokens: 9}, 9, true},
		{"nan", types.EmbeddingUsage{Tokens: math.NaN()}, 0, false},
		{"input only", types.EmbeddingUsage{InputTokens: 4}, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
			settings := &Settings{IsEnabled: Bool(true)}
			rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.embed", Settings: settings})
			integration.OnEmbedStart(rootCtx, EmbeddingModelCallStartEvent{Settings: settings, CallID: "e1", ModelProvider: "p", ModelID: "m"})
			integration.OnEmbedEnd(rootCtx, EmbeddingModelCallEndEvent{Settings: settings, CallID: "e1", Usage: tc.usage})
			span := findSpan(rec, "embeddings m")
			if span == nil {
				t.Fatal("no embeddings span")
			}
			v, ok := attrValue(span, "gen_ai.usage.input_tokens")
			if ok != tc.wantOK || (ok && v.(int64) != tc.want) {
				t.Fatalf("got %v ok=%v, want %d ok=%v", v, ok, tc.want, tc.wantOK)
			}
		})
	}
}
