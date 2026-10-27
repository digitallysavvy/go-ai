package ai

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS LegacyOpenTelemetry.onStepEnd records ai.response.id/model/timestamp
// (and gen_ai.response.id/model) from event.response, and
// ai.response.providerMetadata from event.providerMetadata, on the
// doGenerate/doStream span. Go's text step-end events left them out
// (streamText also left out the provider metadata).
func TestTextStepEndTelemetryCarriesResponseMetadata(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	settings := &TelemetrySettings{
		RecordInputs: telemetry.Bool(true), RecordOutputs: telemetry.Bool(true),
		Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")})},
	}
	meta := json.RawMessage(`{"p":{"k":"v"}}`)
	model := &testutil.MockLanguageModel{
		ModelName: "m",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop, ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"k": "v"}}}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "resp-1", ModelID: "m-resp"}},
				{Type: provider.ChunkTypeText, Text: "ok"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, ProviderMetadata: meta},
			}), nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Telemetry: settings}); err != nil {
		t.Fatal(err)
	}
	stream, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Telemetry: settings})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatal(err)
	}

	attrs := func(name string) map[attribute.Key]attribute.Value {
		for _, s := range rec.Ended() {
			if s.Name() == name {
				out := map[attribute.Key]attribute.Value{}
				for _, kv := range s.Attributes() {
					out[kv.Key] = kv.Value
				}
				return out
			}
		}
		t.Fatalf("no span %q", name)
		return nil
	}
	gen := attrs("ai.generateText.doGenerate")
	if gen["ai.response.id"].AsString() == "" || gen["ai.response.model"].AsString() != "m" || gen["ai.response.timestamp"].AsString() == "" || gen["gen_ai.response.id"].AsString() == "" {
		t.Fatalf("doGenerate span lacks response metadata: %v", gen)
	}
	str := attrs("ai.streamText.doStream")
	if str["ai.response.id"].AsString() != "resp-1" || str["ai.response.model"].AsString() != "m-resp" || str["gen_ai.response.model"].AsString() != "m-resp" {
		t.Fatalf("doStream span lacks response metadata: %v", str)
	}
	if str["ai.response.providerMetadata"].AsString() != `{"p":{"k":"v"}}` {
		t.Fatalf("doStream span providerMetadata = %q", str["ai.response.providerMetadata"].AsString())
	}
}
