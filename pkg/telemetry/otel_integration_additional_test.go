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

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{
		IsEnabled:     Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
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

// TestLegacyOpenTelemetryOnStartBaseAttributes ports TS's
// "should record telemetry data when enabled" legacy-open-telemetry.test.ts
// case (see the matching __snapshots__ entry): the root "ai.generateText"
// span must carry ai.model.provider/id, ai.settings.<key> for every call
// setting including maxRetries, ai.request.headers.<name>, and
// operation.name/resource.name/ai.telemetry.functionId — and its real OTel
// span name must be the bare operation id, never suffixed with functionID
// (follow-up H1, 2026-09-27).
func TestLegacyOpenTelemetryOnStartBaseAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-base-attrs-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{
		IsEnabled:     Bool(true),
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "test-function-id",
	}

	maxTokens := 100
	temp := 0.5
	topP := 0.2
	// TS's snapshot uses a float for topK (0.1); Go's TopK field is *int, so
	// this test exercises the int representation instead.
	topKInt := 1
	presence := 0.4
	frequency := 0.3
	seed := 7
	maxRetries := 2

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType:    "ai.generateText",
		ModelProvider:    "mock-provider",
		ModelID:          "mock-model-id",
		Settings:         settings,
		Prompt:           "prompt",
		Headers:          map[string]string{"header1": "value1", "header2": "value2"},
		MaxOutputTokens:  &maxTokens,
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topKInt,
		PresencePenalty:  &presence,
		FrequencyPenalty: &frequency,
		StopSequences:    []string{"stop"},
		Seed:             &seed,
		MaxRetries:       &maxRetries,
	})
	if !trace.SpanFromContext(ctx).IsRecording() {
		t.Fatal("expected OnStart to embed a recording span in ctx")
	}

	span := findSpan(rec, "ai.generateText")
	if span == nil {
		t.Fatal("expected the real span name to be the bare operation id 'ai.generateText' (not suffixed with functionID)")
	}

	wantStrings := map[string]string{
		"ai.model.provider":          "mock-provider",
		"ai.model.id":                "mock-model-id",
		"ai.request.headers.header1": "value1",
		"ai.request.headers.header2": "value2",
		"ai.telemetry.functionId":    "test-function-id",
		"ai.operationId":             "ai.generateText",
		"operation.name":             "ai.generateText test-function-id",
		"resource.name":              "test-function-id",
	}
	for key, want := range wantStrings {
		if v, ok := attrValue(span, key); !ok || v.(string) != want {
			t.Errorf("%s = %v (ok=%v), want %q", key, v, ok, want)
		}
	}

	wantFloats := map[string]float64{
		"ai.settings.temperature":      0.5,
		"ai.settings.topP":             0.2,
		"ai.settings.presencePenalty":  0.4,
		"ai.settings.frequencyPenalty": 0.3,
	}
	for key, want := range wantFloats {
		if v, ok := attrValue(span, key); !ok || v.(float64) != want {
			t.Errorf("%s = %v (ok=%v), want %v", key, v, ok, want)
		}
	}

	wantInts := map[string]int64{
		"ai.settings.maxOutputTokens": 100,
		"ai.settings.topK":            1,
		"ai.settings.seed":            7,
		"ai.settings.maxRetries":      2,
	}
	for key, want := range wantInts {
		if v, ok := attrValue(span, key); !ok || v.(int64) != want {
			t.Errorf("%s = %v (ok=%v), want %v", key, v, ok, want)
		}
	}

	if v, ok := attrValue(span, "ai.settings.stopSequences"); !ok {
		t.Error("expected ai.settings.stopSequences to be set")
	} else if got := v.([]string); len(got) != 1 || got[0] != "stop" {
		t.Errorf("ai.settings.stopSequences = %v, want [stop]", got)
	}

	// gen_ai.system/gen_ai.request.model must NOT be on the root span — TS's
	// onGenerateStart never sets them there (only the nested doGenerate/
	// doStream step span does).
	if _, ok := attrValue(span, "gen_ai.system"); ok {
		t.Error("root span should not carry gen_ai.system")
	}
	if _, ok := attrValue(span, "gen_ai.request.model"); ok {
		t.Error("root span should not carry gen_ai.request.model")
	}
}

// TestLegacyOpenTelemetryOnStartEmbedRerankAttributes ports TS's
// onEmbedOperationStart/onRerankOperationStart shape: ai.embed uses
// ai.value, ai.embedMany uses ai.values (array of JSON-encoded strings, not
// a count), and ai.rerank uses ai.documents — never ai.prompt.
func TestLegacyOpenTelemetryOnStartEmbedRerankAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-embed-rerank-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true}

	integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.embed",
		ModelProvider: "openai",
		ModelID:       "text-embedding-3-small",
		Settings:      settings,
		Prompt:        "hello world",
	})
	embedSpan := findSpan(rec, "ai.embed")
	if embedSpan == nil {
		t.Fatal("expected an 'ai.embed' span")
	}
	if v, ok := attrValue(embedSpan, "ai.value"); !ok || v.(string) != `"hello world"` {
		t.Errorf("ai.value = %v (ok=%v), want %q", v, ok, `"hello world"`)
	}
	if _, ok := attrValue(embedSpan, "ai.prompt"); ok {
		t.Error("ai.embed span should not carry ai.prompt")
	}

	integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.embedMany",
		ModelProvider: "openai",
		ModelID:       "text-embedding-3-small",
		Settings:      settings,
		Values:        []string{"a", "b"},
	})
	embedManySpan := findSpan(rec, "ai.embedMany")
	if embedManySpan == nil {
		t.Fatal("expected an 'ai.embedMany' span")
	}
	if v, ok := attrValue(embedManySpan, "ai.values"); !ok {
		t.Error("expected ai.values to be set")
	} else if got := v.([]string); len(got) != 2 || got[0] != `"a"` || got[1] != `"b"` {
		t.Errorf("ai.values = %v, want [\"a\" \"b\"]", got)
	}

	integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.rerank",
		ModelProvider: "cohere",
		ModelID:       "rerank-v3.5",
		Settings:      settings,
		Documents:     []string{"doc1", "doc2"},
	})
	rerankSpan := findSpan(rec, "ai.rerank")
	if rerankSpan == nil {
		t.Fatal("expected an 'ai.rerank' span")
	}
	if v, ok := attrValue(rerankSpan, "ai.documents"); !ok {
		t.Error("expected ai.documents to be set")
	} else if got := v.([]string); len(got) != 2 || got[0] != `"doc1"` || got[1] != `"doc2"` {
		t.Errorf("ai.documents = %v, want [\"doc1\" \"doc2\"]", got)
	}
	if _, ok := attrValue(rerankSpan, "ai.prompt"); ok {
		t.Error("ai.rerank span should not carry ai.prompt")
	}
}

func TestOTelIntegrationCustomSpanAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{
		IsEnabled: Bool(true),
		EnrichSpan: func(_ context.Context, opts EnrichSpanOptions) map[string]interface{} {
			return map[string]interface{}{
				"custom.span_type":       string(opts.SpanType),
				"gen_ai.request.model":   "custom-should-not-win",
				"ai.model.id":            "custom-should-not-win",
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
	// The root "operation" span no longer carries gen_ai.request.model (TS's
	// onGenerateStart root span never has gen_ai.* attributes — only the
	// nested doGenerate/doStream step span does), so the collision check for
	// it uses ai.model.id instead, which the root span does set.
	if spansByType["operation"]["ai.model.id"] != "gpt-5" || spansByType["step"]["gen_ai.request.model"] != "gpt-5" {
		t.Fatalf("SDK attributes were not allowed to override custom attributes: %#v", spansByType)
	}
}

func TestOTelIntegrationToolContextParentsNestedOperation(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("telemetry-parentage-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), FunctionID: "outer"}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
	})
	stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
		Settings:      settings,
		OperationType: "ai.generateText",
		StepNumber:    0,
	})
	toolCtx := integration.OnToolExecutionStart(stepCtx, TelemetryToolCallStartEvent{
		Settings:   settings,
		ToolCallID: "tool-call-1",
		ToolName:   "lookup",
	})

	innerSettings := &Settings{IsEnabled: Bool(true), FunctionID: "inner"}
	innerCtx := integration.OnStart(toolCtx, TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      innerSettings,
	})
	integration.OnEnd(innerCtx, TelemetryFinishEvent{Settings: innerSettings, FinishReason: "stop"})

	integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{
		Settings:   settings,
		ToolCallID: "tool-call-1",
		ToolName:   "lookup",
		DurationMs: 1,
	})
	integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{Settings: settings, StepNumber: 0, FinishReason: "tool-calls"})
	integration.OnEnd(rootCtx, TelemetryFinishEvent{Settings: settings, FinishReason: "stop"})

	// The nested "inner" root span is no longer named "ai.generateText.inner"
	// (TS never suffixes the real span name with functionID — see
	// legacyOperationNameAttrs), so it is distinguished from the outer root
	// span (also named "ai.generateText") by its operation.name attribute
	// ("ai.generateText inner") instead of by span name.
	var toolSpan, innerSpan sdktrace.ReadOnlySpan
	for _, span := range rec.Ended() {
		if span.Name() == "ai.toolCall.lookup" {
			toolSpan = span
			continue
		}
		if v, ok := attrValue(span, "operation.name"); ok && v.(string) == "ai.generateText inner" {
			innerSpan = span
		}
	}
	if toolSpan == nil {
		t.Fatal("missing tool execution span")
	}
	if innerSpan == nil {
		t.Fatal("missing nested operation span")
	}
	if got, want := innerSpan.Parent().SpanID(), toolSpan.SpanContext().SpanID(); got != want {
		t.Fatalf("nested operation parent span = %s, want tool span %s", got, want)
	}
}

func TestGetTracerPaths(t *testing.T) {
	if GetTracer(&Settings{IsEnabled: Bool(false)}) == nil {
		t.Fatal("GetTracer(disabled) should return a tracer")
	}
	if GetTracer(&Settings{IsEnabled: Bool(true)}) == nil {
		t.Fatal("GetTracer(enabled) should return the global tracer")
	}
	if GetTracer(nil) == nil {
		t.Fatal("GetTracer(nil) should return global tracer")
	}
}

// TestLegacyOpenTelemetryConstructorTracer covers 9b47dea: a
// NewLegacyOpenTelemetry-constructed integration uses its own tracer even
// though Settings no longer carries one.
func TestLegacyOpenTelemetryConstructorTracer(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("ctor-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      &Settings{IsEnabled: Bool(true)},
	})
	trace.SpanFromContext(ctx).End()
	if len(rec.Ended()) != 1 {
		t.Fatalf("expected the constructor's tracer to record 1 span, got %d", len(rec.Ended()))
	}
}
