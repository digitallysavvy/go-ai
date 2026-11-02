package ai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 79 carry: TS records `'ai.usage.tokens': event.usage.tokens` on both
// the legacy ai.embed root and its ai.embed.doEmbed span, so the two always
// agree. Go's doEmbed span fell back to InputTokens for a model that reports
// only InputTokens, while the root read only Tokens and TotalTokens and
// dropped the count.
func TestR79Carry_EmbedRootAndDoEmbedRecordSameUsageTokens(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	legacy := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry"),
	})
	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{Embedding: []float64{0.1}, Usage: types.EmbeddingUsage{InputTokens: 7}}, nil
		},
	}
	if _, err := Embed(context.Background(), EmbedOptions{
		Model: model, Input: "x",
		Telemetry: &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{legacy}},
	}); err != nil {
		t.Fatal(err)
	}
	got := map[string]interface{}{}
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.usage.tokens" {
				got[s.Name()] = kv.Value.AsInterface()
			}
		}
	}
	for _, name := range []string{"ai.embed", "ai.embed.doEmbed"} {
		if v, ok := got[name]; !ok || v != float64(7) && v != int64(7) {
			t.Fatalf("%s ai.usage.tokens = %v (set %v), want 7; all: %v", name, v, ok, got)
		}
	}
}
