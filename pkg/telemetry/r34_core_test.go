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
			rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{CallID: "e1", OperationType: "ai.embed", Settings: settings})
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

// JSON.stringify leaves <, > and & literal; the span attributes must not carry
// Go's HTML escapes (<).
func TestDecideSpanAttributesAreNotHTMLEscaped(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(true), RecordOutputs: Bool(true)}
	integration.OnDecideStart(context.Background(), DecideStartEvent{
		Settings: settings, CallID: "d1", OperationID: "ai.decide",
		State: map[string]string{"q": "a<b>&c"},
	})
	integration.OnDecideEnd(context.Background(), DecideEndEvent{DecideStartEvent: DecideStartEvent{Settings: settings, CallID: "d1"}})
	span := findSpan(rec, "ai.decide")
	if span == nil {
		t.Fatal("no ai.decide span")
	}
	v, ok := attrValue(span, "ai.decision.state")
	if !ok || v.(string) != `{"q":"a<b>&c"}` {
		t.Fatalf("ai.decision.state = %v ok=%v", v, ok)
	}
}

func TestLegacyOpenTelemetryEmbedUsageTokens(t *testing.T) {
	for _, tc := range []struct {
		name   string
		usage  types.EmbeddingUsage
		want   float64
		wantOK bool
	}{
		{"tokens", types.EmbeddingUsage{Tokens: 7}, 7, true},
		{"input only", types.EmbeddingUsage{InputTokens: 3}, 3, true},
		{"nan", types.EmbeddingUsage{Tokens: math.NaN()}, 0, false},
		{"absent", types.EmbeddingUsage{}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
			settings := &Settings{IsEnabled: Bool(true)}
			rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.embed", CallID: "c", Settings: settings})
			integration.OnEmbedStart(rootCtx, EmbeddingModelCallStartEvent{Settings: settings, CallID: "c", EmbedCallID: "e1", OperationID: "ai.embed.doEmbed"})
			integration.OnEmbedEnd(rootCtx, EmbeddingModelCallEndEvent{Settings: settings, CallID: "c", EmbedCallID: "e1", OperationID: "ai.embed.doEmbed", Usage: tc.usage})
			span := findSpan(rec, "ai.embed.doEmbed")
			if span == nil {
				t.Fatal("no span")
			}
			v, ok := attrValue(span, "ai.usage.tokens")
			if ok != tc.wantOK || (ok && v.(float64) != tc.want) {
				t.Fatalf("got %v ok=%v, want %v ok=%v", v, ok, tc.want, tc.wantOK)
			}
		})
	}
}
