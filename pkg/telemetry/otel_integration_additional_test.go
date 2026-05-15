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
	ctx = integration.OnToolCallStart(ctx, TelemetryToolCallStartEvent{
		Settings:   settings,
		ToolCallID: "call-1",
		ToolName:   "lookup_weather",
	})
	integration.OnToolCallFinish(ctx, TelemetryToolCallFinishEvent{
		Settings:   settings,
		ToolCallID: "call-1",
		ToolName:   "lookup_weather",
		DurationMs: 12,
		Error:      errors.New("tool failed"),
	})
	integration.OnChunk(ctx, TelemetryChunkEvent{Settings: settings, ChunkType: "text", Text: "partial"})

	ts := time.Now().UTC()
	integration.OnStepFinish(ctx, TelemetryStepFinishEvent{
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
