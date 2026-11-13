package telemetry

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A provider number beyond the double range decodes as +Inf (JSON.parse
// Infinity) and the telemetry attribute JSON writes it as null, as
// JSON.stringify does; json.Marshal would fail and drop the attribute.
func TestR96carryProviderMetadataInfinityAttributeIsNull(t *testing.T) {
	var metadata map[string]interface{}
	if err := jsonexact.Unmarshal([]byte(`{"openai":{"score":1e400,"n":1}}`), &metadata); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("r96"), ProviderMetadata: true})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "c"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "c"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "c", ModelID: "m"})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "c", FinishReason: "stop", ProviderMetadata: metadata})
	span := findSpan(rec, "chat m")
	if span == nil {
		t.Fatal("no chat span")
	}
	v, ok := attrValue(span, "ai.response.providerMetadata")
	if !ok || v != `{"openai":{"n":1,"score":null}}` {
		t.Fatalf("providerMetadata = %v (ok %v)", v, ok)
	}
}
