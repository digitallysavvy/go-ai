package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// The legacy loop's step and root events carry the step's reasoning files,
// so a step whose only reasoning is a file records ai.response.reasoning
// (TS reasoning.length > 0 counts reasoning file parts).
func TestR50LoopTelemetryReasoningFileOnly(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	a := NewToolLoopAgent(AgentConfig{
		Model: &mockLanguageModel{responses: []types.GenerateResult{{
			Text:         "hi",
			Content:      []types.ContentPart{types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1}}, types.TextContent{Text: "hi"}},
			FinishReason: types.FinishReasonStop,
		}}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ai.generateText.doGenerate", "ai.generateText"} {
		found := false
		for _, s := range rec.Ended() {
			if s.Name() != name {
				continue
			}
			found = true
			ok := false
			for _, kv := range s.Attributes() {
				if string(kv.Key) == "ai.response.reasoning" && kv.Value.AsString() == "" {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s has no empty ai.response.reasoning", name)
			}
		}
		if !found {
			t.Fatalf("no %s span", name)
		}
	}
}
