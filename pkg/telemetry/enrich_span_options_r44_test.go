package telemetry

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS getSpanAttributes passes enrichSpan the root operation id, the call id
// and state.runtimeContext for every child span. Go passed "ai.generateText"
// for every chat span, the tool call id as the tool span's call id, the
// embedding operation id, and no call id or runtime context on several spans.
func TestOpenTelemetryEnrichSpanOptionsMatchTS(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	var mu sync.Mutex
	seen := map[SpanType][]EnrichSpanOptions{}
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t"), EnrichSpan: func(_ context.Context, o EnrichSpanOptions) map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		seen[o.SpanType] = append(seen[o.SpanType], o)
		return nil
	}})
	settings := &Settings{RecordInputs: true, RecordOutputs: true, Integrations: []TelemetryIntegration{integration}}
	callID := "call-enrich-r44"
	rc := map[string]interface{}{"tenant": "a"}
	ctx := FireOnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.streamText", ModelProvider: "p", ModelID: "m", Settings: settings, RuntimeContext: rc})
	stepCtx := FireOnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, Settings: settings, OperationType: "ai.streamText", ModelProvider: "p", ModelID: "m", RuntimeContext: rc})
	lmCtx := FireOnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{CallID: callID, Settings: settings, ModelProvider: "p", ModelID: "m"})
	FireOnLanguageModelCallEnd(lmCtx, LanguageModelCallEndEvent{CallID: callID, Settings: settings, ModelProvider: "p", ModelID: "m", FinishReason: "stop",
		Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "pe1", ToolName: "web_search", ProviderExecuted: true}}})
	toolCtx := FireOnToolCallStart(stepCtx, TelemetryToolCallStartEvent{CallID: callID, Settings: settings, ToolCallID: "t1", ToolName: "tool"})
	FireOnToolCallFinish(toolCtx, TelemetryToolCallFinishEvent{CallID: callID, Settings: settings, ToolCallID: "t1", ToolName: "tool", Result: "ok"})
	FireOnStepEnd(stepCtx, TelemetryStepEndEvent{CallID: callID, Settings: settings, OperationType: "ai.streamText", FinishReason: "stop"})
	FireOnEnd(ctx, TelemetryFinishEvent{CallID: callID, Settings: settings, OperationType: "ai.streamText", FinishReason: "stop"})

	want := EnrichSpanOptions{OperationType: "ai.streamText", CallID: callID, RuntimeContext: rc}
	for _, spanType := range []SpanType{SpanTypeOperation, SpanTypeStep, SpanTypeLanguageModel, SpanTypeTool} {
		got := seen[spanType]
		wantCount := 1
		if spanType == SpanTypeTool {
			wantCount = 2 // the provider-executed tool span and the local one
		}
		if len(got) != wantCount {
			t.Fatalf("%s: enrichSpan calls = %+v, want %d", spanType, got, wantCount)
		}
		for _, o := range got {
			w := want
			w.SpanType = spanType
			if !reflect.DeepEqual(o, w) {
				t.Errorf("%s: enrichSpan options = %+v, want %+v", spanType, o, w)
			}
		}
	}
}

func TestOpenTelemetryEmbedEnrichSpanOptionsUseRootOperation(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	var got []EnrichSpanOptions
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t"), EnrichSpan: func(_ context.Context, o EnrichSpanOptions) map[string]interface{} {
		if o.SpanType == SpanTypeEmbedding {
			got = append(got, o)
		}
		return nil
	}})
	settings := &Settings{Integrations: []TelemetryIntegration{integration}}
	callID := "call-embed-enrich-r44"
	ctx := FireOnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.embedMany", ModelProvider: "p", ModelID: "m", Settings: settings})
	FireOnEmbedStart(ctx, EmbeddingModelCallStartEvent{CallID: callID, EmbedCallID: "e1", OperationID: "ai.embedMany.doEmbed", Settings: settings, ModelProvider: "p", ModelID: "m"})
	FireOnEmbedEnd(ctx, EmbeddingModelCallEndEvent{CallID: callID, EmbedCallID: "e1", OperationID: "ai.embedMany.doEmbed", Settings: settings})
	FireOnEnd(ctx, TelemetryFinishEvent{CallID: callID, Settings: settings, OperationType: "ai.embedMany"})
	if len(got) != 1 || got[0].OperationType != "ai.embedMany" || got[0].CallID != callID {
		t.Fatalf("embedding enrichSpan options = %+v", got)
	}
}
