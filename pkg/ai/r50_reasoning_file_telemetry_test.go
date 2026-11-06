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

// TS StepResult.reasoning holds reasoning file parts too, so a step whose
// only reasoning is a reasoning file records ai.response.reasoning (the
// joined text of its text parts: "") on the legacy step and root spans.
func TestR50LegacyReasoningRecordedForReasoningFileOnly(t *testing.T) {
	file := types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1, 2, 3}}
	for _, stream := range []bool{false, true} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		tel := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}}
		model := &testutil.MockLanguageModel{
			ProviderName: "google.generative-ai", ModelName: "gemini",
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Text:         "hi",
					Content:      []types.ContentPart{file, types.TextContent{Text: "hi"}},
					FinishReason: types.FinishReasonStop,
				}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				f := file
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &f},
					{Type: provider.ChunkTypeText, Text: "hi"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		}
		stepSpan, rootSpan := "ai.generateText.doGenerate", "ai.generateText"
		if stream {
			stepSpan, rootSpan = "ai.streamText.doStream", "ai.streamText"
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
		for _, name := range []string{stepSpan, rootSpan} {
			span := findSpanByName(rec.Ended(), name)
			if span == nil {
				t.Fatalf("stream=%v: no %s span", stream, name)
			}
			if v, ok := attrValueTelemetry(span, "ai.response.reasoning"); !ok || v != "" {
				t.Errorf("stream=%v: %s ai.response.reasoning = %q (ok=%v), want \"\"", stream, name, v, ok)
			}
		}
		_ = tp.Shutdown(context.Background())
	}
}
