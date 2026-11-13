package telemetry

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r80carryUsageCases: owner ruling 2026-11-02 (option b). TS records
// usage.tokens, so a provider-reported 0 is recorded; a usage the provider
// did not report (TS NaN) or a NaN count is dropped, and an unmarked 0 keeps
// the v0.5 "unset" reading.
var r80carryUsageCases = []struct {
	name   string
	usage  types.EmbeddingUsage
	want   int64
	wantOK bool
}{
	{"reported 0", types.EmbeddingUsage{Reported: true}, 0, true},
	{"reported 5", types.EmbeddingUsage{Tokens: 5, InputTokens: 5, TotalTokens: 5, Reported: true}, 5, true},
	{"reported NaN", types.EmbeddingUsage{Tokens: math.NaN(), Reported: true}, 0, false},
	{"unreported 0", types.EmbeddingUsage{}, 0, false},
	{"unreported 4", types.EmbeddingUsage{InputTokens: 4}, 4, true},
}

func TestR80carryOpenTelemetryEmbedUsageReported(t *testing.T) {
	for _, tc := range r80carryUsageCases {
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

func TestR80carryLegacyOpenTelemetryEmbedUsageReported(t *testing.T) {
	for _, tc := range r80carryUsageCases {
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
			if ok != tc.wantOK || (ok && v.(int64) != tc.want) {
				t.Fatalf("got %v ok=%v, want %d ok=%v", v, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// Reviewer item 4 (r80): FireOnStepError publishes a diagnostic message like
// every other Fire* (TS dispatches onStepError through the same telemetry
// dispatcher as the other lifecycle events); disabled telemetry publishes
// nothing.
func TestR80carryFireOnStepErrorPublishesDiagnostic(t *testing.T) {
	var mu sync.Mutex
	var got []TelemetryErrorEvent
	unsubscribe := SubscribeDiagnosticTyped(DiagnosticEventOnStepError, func(_ context.Context, e TelemetryErrorEvent) error {
		mu.Lock()
		defer mu.Unlock()
		if e.CallID == "r80carry-step-error" || e.CallID == "r80carry-step-error-off" {
			got = append(got, e)
		}
		return nil
	})
	defer unsubscribe()
	boom := errors.New("boom")
	FireOnStepError(context.Background(), TelemetryErrorEvent{Settings: &Settings{IsEnabled: Bool(true)}, CallID: "r80carry-step-error", Error: boom})
	FireOnStepError(context.Background(), TelemetryErrorEvent{Settings: &Settings{IsEnabled: Bool(false)}, CallID: "r80carry-step-error-off", Error: boom})
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].CallID != "r80carry-step-error" || !errors.Is(got[0].Error, boom) || got[0].AIVersion == "" {
		t.Fatalf("published = %+v, want one enabled event with the error and AIVersion", got)
	}
}
