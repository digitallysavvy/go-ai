package ai

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Owner decision (2026-10-28): without a tracer of their own, the legacy
// integration uses the global tracer named "ai" (TS trace.getTracer('ai')) and
// the GenAI integration the one named "gen_ai" (TS trace.getTracer('gen_ai')).
// A caller-supplied tracer is used as is.
func TestTelemetryDefaultTracerNamesLikeTS(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls%2 == 1 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "echo", Arguments: map[string]interface{}{}}}}, nil
			}
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}
	echo := types.Tool{Name: "echo", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	custom := tp.Tracer("custom-tracer")
	for _, tc := range []struct {
		name        string
		integration telemetry.TelemetryIntegration
		wantScope   string
	}{
		{"legacy default", telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{}), telemetry.LegacyTracerName},
		{"legacy zero value", telemetry.LegacyOpenTelemetry{}, "ai"},
		{"genai default", telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{}), "gen_ai"},
		{"legacy custom", telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: custom}), "custom-tracer"},
		{"genai custom", telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: custom}), "custom-tracer"},
	} {
		before := len(rec.Ended())
		if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "go", Tools: []types.Tool{echo}, StopWhen: []StopCondition{IsStepCount(2)},
			Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{tc.integration}},
		}); err != nil {
			t.Fatal(err)
		}
		spans := rec.Ended()[before:]
		names := map[string]bool{}
		for _, s := range spans {
			names[s.Name()] = true
		}
		if len(spans) < 4 || (!names["ai.toolCall"] && !names["execute_tool echo"]) {
			t.Fatalf("%s: spans %v", tc.name, names)
		}
		for _, s := range spans {
			if got := s.InstrumentationScope().Name; got != tc.wantScope {
				t.Errorf("%s: span %q tracer = %q, want %q", tc.name, s.Name(), got, tc.wantScope)
			}
		}
	}
	// Child spans of other operations use the root's tracer too (TS
	// this.tracer): the legacy embed root and its doEmbed span.
	before := len(rec.Ended())
	emb := &testutil.MockEmbeddingModel{ProviderName: "p", ModelName: "m", DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
		return &types.EmbeddingResult{Embedding: []float64{1}}, nil
	}}
	if _, err := Embed(context.Background(), EmbedOptions{Model: emb, Input: "x", Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: custom}),
	}}}); err != nil {
		t.Fatal(err)
	}
	embedSpans := rec.Ended()[before:]
	if len(embedSpans) < 2 {
		t.Fatalf("embed spans = %d", len(embedSpans))
	}
	for _, s := range embedSpans {
		if got := s.InstrumentationScope().Name; got != "custom-tracer" {
			t.Errorf("embed span %q tracer = %q", s.Name(), got)
		}
	}
	if telemetry.GenAITracerName != "gen_ai" || telemetry.LegacyTracerName != "ai" {
		t.Errorf("tracer names = %q, %q", telemetry.LegacyTracerName, telemetry.GenAITracerName)
	}
}
