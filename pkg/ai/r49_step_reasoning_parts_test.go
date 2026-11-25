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

// GenerateText and StreamText step-end events carry the step's reasoning
// parts, so the legacy step span records ai.response.reasoning for a
// reasoning part with empty text (TS reasoning.length > 0).
func TestR49StepEndCarriesEmptyReasoningParts(t *testing.T) {
	for _, stream := range []bool{false, true} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		tel := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}}
		model := &testutil.MockLanguageModel{
			ProviderName: "openai.chat", ModelName: "gpt-4",
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Text:         "hi",
					Content:      []types.ContentPart{types.ReasoningContent{Text: ""}, types.TextContent{Text: "hi"}},
					FinishReason: types.FinishReasonStop,
				}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningStart, ID: "r"},
					{Type: provider.ChunkTypeReasoningEnd, ID: "r"},
					{Type: provider.ChunkTypeText, Text: "hi"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		}
		stepSpan := "ai.generateText.doGenerate"
		if stream {
			stepSpan = "ai.streamText.doStream"
			result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "Hello", Telemetry: tel})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "Hello", Telemetry: tel}); err != nil {
			t.Fatal(err)
		}
		span := findSpanByName(rec.Ended(), stepSpan)
		if span == nil {
			t.Fatalf("stream=%v: no %s span", stream, stepSpan)
		}
		if v, ok := attrValueTelemetry(span, "ai.response.reasoning"); !ok || v != "" {
			t.Errorf("stream=%v: %s ai.response.reasoning = %q (ok=%v), want \"\"", stream, stepSpan, v, ok)
		}
		_ = tp.Shutdown(context.Background())
	}
}
