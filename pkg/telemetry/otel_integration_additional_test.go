package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func int64p(v int64) *int64 { return &v }

func TestOTelIntegrationStepFinishFinishAndError(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{
		IsEnabled:     Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		Tracer:        tracer,
		FunctionID:    "fn-id",
	}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		ModelProvider: "openai",
		ModelID:       "gpt-5",
		Prompt:        "hello",
		Settings:      settings,
	})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{
		Settings:      settings,
		OperationType: "ai.generateText",
		StepNumber:    1,
		ModelProvider: "openai",
		ModelID:       "gpt-5",
	})
	ctx = integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{
		Settings:   settings,
		ToolCallID: "call-1",
		ToolName:   "lookup_weather",
	})
	integration.OnToolExecutionEnd(ctx, TelemetryToolCallFinishEvent{
		Settings:   settings,
		ToolCallID: "call-1",
		ToolName:   "lookup_weather",
		DurationMs: 12,
		Error:      errors.New("tool failed"),
	})
	integration.OnChunk(ctx, TelemetryChunkEvent{Settings: settings, ChunkType: "text", Text: "partial"})

	ts := time.Now().UTC()
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{
		Settings:          settings,
		StepNumber:        1,
		FinishReason:      "stop",
		Text:              "answer text",
		Reasoning:         "chain",
		ProviderMetadata:  map[string]interface{}{"provider": "meta"},
		ResponseID:        "resp-id",
		ResponseModelID:   "gpt-5",
		ResponseTimestamp: ts,
		ToolCalls:         []types.ToolCall{{ID: "call-1", ToolName: "lookup_weather", Arguments: map[string]interface{}{"city": "Paris"}}},
		Files:             []types.GeneratedFileContent{{MediaType: "image/png", Data: []byte{1, 2, 3}}},
		Usage: TelemetryUsage{
			InputTokens:              int64p(10),
			OutputTokens:             int64p(5),
			TotalTokens:              int64p(15),
			ReasoningTokens:          int64p(2),
			CacheReadInputTokens:     int64p(1),
			CacheCreationInputTokens: int64p(3),
			NoCacheInputTokens:       int64p(6),
			OutputTextTokens:         int64p(4),
		},
	})

	integration.OnFinish(ctx, TelemetryFinishEvent{
		Settings:      settings,
		FinishReason:  "stop",
		ModelProvider: "openai",
		ModelID:       "gpt-5",
		Text:          "final text",
		Files:         []types.GeneratedFileContent{{MediaType: "image/png", Data: []byte{4, 5}}},
		Usage: TelemetryUsage{
			InputTokens:              int64p(10),
			OutputTokens:             int64p(5),
			TotalTokens:              int64p(15),
			ReasoningTokens:          int64p(2),
			CacheReadInputTokens:     int64p(1),
			CacheCreationInputTokens: int64p(3),
			NoCacheInputTokens:       int64p(6),
			OutputTextTokens:         int64p(4),
		},
	})

	// Separate run for explicit error path while root span is recording.
	errCtx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
	})
	integration.OnError(errCtx, TelemetryErrorEvent{
		Settings: settings,
		Error:    errors.New("root failure"),
	})

	if len(rec.Ended()) == 0 {
		t.Fatal("expected ended spans to be recorded")
	}
}

func TestOTelIntegrationCustomSpanAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := OTelTelemetryIntegration{}
	settings := &Settings{
		IsEnabled: Bool(true),
		Tracer:    tracer,
		EnrichSpan: func(_ context.Context, opts EnrichSpanOptions) map[string]interface{} {
			return map[string]interface{}{
				"custom.span_type":       string(opts.SpanType),
				"gen_ai.request.model":   "custom-should-not-win",
				"custom.runtime_present": opts.RuntimeContext["user"] == "alice",
				"custom.call_id":         opts.CallID,
			}
		},
	}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType:  "ai.generateText",
		ModelProvider:  "openai",
		ModelID:        "gpt-5",
		Settings:       settings,
		RuntimeContext: map[string]interface{}{"user": "alice"},
	})
	stepCtx := integration.OnStepStart(ctx, TelemetryStepStartEvent{
		Settings:       settings,
		OperationType:  "ai.generateText",
		StepNumber:     0,
		ModelProvider:  "openai",
		ModelID:        "gpt-5",
		RuntimeContext: map[string]interface{}{"user": "alice"},
	})
	integration.OnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{
		Settings:      settings,
		CallID:        "lm-1",
		ModelProvider: "openai",
		ModelID:       "gpt-5",
	})
	integration.OnLanguageModelCallEnd(stepCtx, LanguageModelCallEndEvent{
		Settings:     settings,
		CallID:       "lm-1",
		FinishReason: "stop",
		Performance: LanguageModelCallPerformance{
			ResponseTimeMs:                 10,
			EffectiveOutputTokensPerSecond: 20,
			EffectiveTotalTokensPerSecond:  30,
		},
	})
	toolCtx := integration.OnToolExecutionStart(stepCtx, TelemetryToolCallStartEvent{
		Settings:   settings,
		ToolCallID: "call-1",
		ToolName:   "lookup",
	})
	integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{
		Settings:   settings,
		ToolCallID: "call-1",
		ToolName:   "lookup",
		DurationMs: 1,
	})
	integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{
		Settings:     settings,
		StepNumber:   0,
		FinishReason: "tool-calls",
	})
	integration.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{
		Settings:      settings,
		CallID:        "embed-1",
		OperationID:   "ai.embed",
		ModelProvider: "openai",
		ModelID:       "text-embedding-3-small",
	})
	integration.OnEmbedEnd(ctx, EmbeddingModelCallEndEvent{
		Settings:    settings,
		CallID:      "embed-1",
		OperationID: "ai.embed",
		Embeddings:  [][]float64{{1, 2}},
		Usage:       types.EmbeddingUsage{InputTokens: 3, TotalTokens: 3},
	})
	integration.OnRerankStart(ctx, RerankingModelCallStartEvent{
		Settings:      settings,
		CallID:        "rerank-1",
		OperationID:   "ai.rerank",
		ModelProvider: "cohere",
		ModelID:       "rerank-v3.5",
	})
	integration.OnRerankEnd(ctx, RerankingModelCallEndEvent{
		Settings:    settings,
		CallID:      "rerank-1",
		OperationID: "ai.rerank",
		Ranking:     []types.RerankItem{{Index: 0, RelevanceScore: 0.9}},
	})
	integration.OnEnd(ctx, TelemetryFinishEvent{Settings: settings, FinishReason: "stop"})

	ended := rec.Ended()
	if len(ended) != 6 {
		t.Fatalf("ended spans = %d, want 6", len(ended))
	}

	spansByType := map[string]map[string]interface{}{}
	for _, span := range ended {
		attrs := map[string]interface{}{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}
		if spanType, ok := attrs["custom.span_type"].(string); ok {
			spansByType[spanType] = attrs
		}
	}
	for _, spanType := range []string{"operation", "step", "languageModel", "tool", "embedding", "reranking"} {
		if spansByType[spanType] == nil {
			t.Fatalf("missing custom attributes for %s span: %#v", spanType, spansByType)
		}
	}
	if spansByType["operation"]["custom.runtime_present"] != true || spansByType["step"]["custom.runtime_present"] != true {
		t.Fatalf("runtime context was not passed to operation and step enrichers: %#v", spansByType)
	}
	if spansByType["tool"]["custom.call_id"] != "call-1" {
		t.Fatalf("tool call id was not passed to tool enricher: %#v", spansByType["tool"])
	}
	if spansByType["languageModel"]["custom.call_id"] != "lm-1" || spansByType["embedding"]["custom.call_id"] != "embed-1" || spansByType["reranking"]["custom.call_id"] != "rerank-1" {
		t.Fatalf("model call ids were not passed to enrichers: %#v", spansByType)
	}
	if spansByType["operation"]["gen_ai.request.model"] != "gpt-5" || spansByType["step"]["gen_ai.request.model"] != "gpt-5" {
		t.Fatalf("SDK attributes were not allowed to override custom attributes: %#v", spansByType)
	}
}

func TestGetTracerPaths(t *testing.T) {
	custom := trace.NewNoopTracerProvider().Tracer("custom")
	if GetTracer(&Settings{IsEnabled: Bool(false)}) == nil {
		t.Fatal("GetTracer(disabled) should return a tracer")
	}
	if got := GetTracer(&Settings{Tracer: custom}); got == nil {
		t.Fatal("GetTracer(custom) should return custom tracer")
	}
	if GetTracer(nil) == nil {
		t.Fatal("GetTracer(nil) should return global tracer")
	}
}
