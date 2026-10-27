package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// TS onStepEnd records event.response.id/modelId/timestamp on the
// doGenerate span; the legacy loop's step-end event left them out.
func TestGenerateAgentStepEndTelemetryCarriesResponseMetadata(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	a := NewToolLoopAgent(AgentConfig{
		Model: &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop}}},
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})},
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range rec.Ended() {
		if s.Name() != "ai.generateText.doGenerate" {
			continue
		}
		got := map[string]string{}
		for _, kv := range s.Attributes() {
			got[string(kv.Key)] = kv.Value.String()
		}
		for _, key := range []string{"ai.response.id", "ai.response.model", "ai.response.timestamp", "gen_ai.response.id"} {
			if got[key] == "" {
				t.Errorf("doGenerate span lacks %s (%v)", key, got)
			}
		}
		return
	}
	t.Fatal("no doGenerate span")
}

// TS onGenerateEnd records ai.response.files from every step's files on the
// root span; the legacy loop's end event carried none.
func TestGenerateAgentEndTelemetryCarriesFiles(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	a := NewToolLoopAgent(AgentConfig{
		Model: &mockLanguageModel{responses: []types.GenerateResult{{
			Text: "done", FinishReason: types.FinishReasonStop,
			Content: []types.ContentPart{types.TextContent{Text: "done"}, types.GeneratedFileContent{MediaType: "image/png", Data: []byte{1, 2}}},
		}}},
		Telemetry: &telemetry.Options{
			IsEnabled:     telemetry.Bool(true),
			RecordOutputs: true,
			Integrations:  []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})},
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range rec.Ended() {
		if s.Name() != "ai.generateText" {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.response.files" && kv.Value.AsString() == `[{"type":"file","mediaType":"image/png","data":"AQI="}]` {
				return
			}
		}
		t.Fatalf("root span lacks ai.response.files: %v", s.Attributes())
	}
	t.Fatal("no root span")
}
