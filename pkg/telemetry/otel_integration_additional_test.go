package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
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

// TestLegacyOpenTelemetryOnStartPromptJSON ports TS's legacy-open-telemetry
// snapshot shapes for ai.prompt: onGenerateStart's `JSON.stringify({system,
// messages})` for generateText/streamText (a `prompt` string call option
// normalizes into a single user message) and onObjectOperationStart's
// `JSON.stringify({system, prompt, messages})` for generateObject/
// streamObject (prompt/messages passed through as raw, mutually-exclusive
// call options, unlike generateText's always-normalized messages).
func TestLegacyOpenTelemetryOnStartPromptJSON(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-prompt-json-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true}

	// generateText: `event.messages` is always the normalized message list,
	// even though this event is built directly here (bypassing pkg/ai's
	// prompt-to-messages normalization) with an explicit Messages value —
	// mirroring a caller that used the `messages` option directly.
	integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prompt"}}},
		},
	})
	textSpan := findSpan(rec, "ai.generateText")
	if textSpan == nil {
		t.Fatal("expected an 'ai.generateText' span")
	}
	wantTextPrompt := `{"messages":[{"role":"user","content":[{"type":"text","text":"prompt"}]}]}`
	if v, ok := attrValue(textSpan, "ai.prompt"); !ok || v.(string) != wantTextPrompt {
		t.Errorf("generateText ai.prompt = %v (ok=%v), want %q", v, ok, wantTextPrompt)
	}

	// generateObject: prompt/messages are raw, mutually-exclusive call
	// options — a bare `prompt` string produces `{"prompt":"..."}` with no
	// "messages" key at all (TS: `JSON.stringify({system, prompt,
	// messages})` drops the undefined `messages` field).
	integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateObject",
		Settings:      settings,
		Prompt:        "prompt",
	})
	objectSpan := findSpan(rec, "ai.generateObject")
	if objectSpan == nil {
		t.Fatal("expected an 'ai.generateObject' span")
	}
	wantObjectPrompt := `{"prompt":"prompt"}`
	if v, ok := attrValue(objectSpan, "ai.prompt"); !ok || v.(string) != wantObjectPrompt {
		t.Errorf("generateObject ai.prompt = %v (ok=%v), want %q", v, ok, wantObjectPrompt)
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

// TestLegacyOpenTelemetryNestedEmbedRerankSpans ports TS's
// legacy-open-telemetry.test.ts rerank "should record telemetry data when
// enabled" snapshot (name: "ai.rerank.doRerank", ai.operationId/
// operation.name: "ai.rerank.doRerank", ai.documents/ai.ranking/
// ai.ranking.type, ai.model.provider/id + ai.settings.maxRetries reused from
// the root span's base attributes, no gen_ai.*) and the analogous
// onEmbedStart/onEmbedEnd shape for "ai.embed.doEmbed" (H3 follow-up 2: the
// nested spans were previously named "embeddings <model>"/
// "reranking <model>" and carried gen_ai.* instead).
func TestLegacyOpenTelemetryNestedEmbedRerankSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-nested-embed-rerank-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}
	maxRetries := 2

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.embed",
		ModelProvider: "openai",
		ModelID:       "text-embedding-3-small",
		Settings:      settings,
		Prompt:        "hello world",
		MaxRetries:    &maxRetries,
	})
	integration.OnEmbedStart(rootCtx, EmbeddingModelCallStartEvent{
		Settings: settings, CallID: "embed-call", EmbedCallID: "embed-1",
		OperationID: "ai.embed.doEmbed", ModelProvider: "openai", ModelID: "text-embedding-3-small",
		Values: []string{"hello world"},
	})
	integration.OnEmbedEnd(rootCtx, EmbeddingModelCallEndEvent{
		Settings: settings, CallID: "embed-call", EmbedCallID: "embed-1",
		OperationID: "ai.embed.doEmbed", ModelProvider: "openai", ModelID: "text-embedding-3-small",
		Embeddings: [][]float64{{0.1, 0.2}},
		Usage:      types.EmbeddingUsage{Tokens: 5},
	})

	doEmbedSpan := findSpan(rec, "ai.embed.doEmbed")
	if doEmbedSpan == nil {
		t.Fatal(`expected a span literally named "ai.embed.doEmbed" (not "embeddings text-embedding-3-small")`)
	}
	if v, ok := attrValue(doEmbedSpan, "ai.operationId"); !ok || v.(string) != "ai.embed.doEmbed" {
		t.Errorf("ai.operationId = %v (ok=%v), want ai.embed.doEmbed", v, ok)
	}
	if v, ok := attrValue(doEmbedSpan, "ai.model.provider"); !ok || v.(string) != "openai" {
		t.Errorf("expected ai.model.provider reused from the root span's base attrs, got %v (ok=%v)", v, ok)
	}
	if v, ok := attrValue(doEmbedSpan, "ai.settings.maxRetries"); !ok || v.(int64) != 2 {
		t.Errorf("expected ai.settings.maxRetries=2 reused from the root span's base attrs, got %v (ok=%v)", v, ok)
	}
	if v, ok := attrValue(doEmbedSpan, "ai.values"); !ok {
		t.Error("expected ai.values to be set")
	} else if got := v.([]string); len(got) != 1 || got[0] != `"hello world"` {
		t.Errorf("ai.values = %v, want [\"hello world\"]", got)
	}
	if v, ok := attrValue(doEmbedSpan, "ai.embeddings"); !ok {
		t.Error("expected ai.embeddings to be set")
	} else if got := v.([]string); len(got) != 1 || got[0] != `[0.1,0.2]` {
		t.Errorf("ai.embeddings = %v, want [[0.1,0.2]]", got)
	}
	if v, ok := attrValue(doEmbedSpan, "ai.usage.tokens"); !ok || v.(float64) != 5 {
		t.Errorf("ai.usage.tokens = %v (ok=%v), want 5", v, ok)
	}
	for _, key := range []string{"gen_ai.operation.name", "gen_ai.system", "gen_ai.request.model"} {
		if _, ok := attrValue(doEmbedSpan, key); ok {
			t.Errorf("ai.embed.doEmbed span should not carry %s (TS has no gen_ai.* here)", key)
		}
	}

	rerankRootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.rerank",
		ModelProvider: "cohere",
		ModelID:       "rerank-v3.5",
		Settings:      settings,
		Documents:     []string{"doc1", "doc2"},
		MaxRetries:    &maxRetries,
	})
	integration.OnRerankStart(rerankRootCtx, RerankingModelCallStartEvent{
		Settings: settings, CallID: "rerank-call", OperationID: "ai.rerank.doRerank",
		ModelProvider: "cohere", ModelID: "rerank-v3.5", Documents: []string{"doc1", "doc2"},
	})
	integration.OnRerankEnd(rerankRootCtx, RerankingModelCallEndEvent{
		Settings: settings, CallID: "rerank-call", OperationID: "ai.rerank.doRerank",
		ModelProvider: "cohere", ModelID: "rerank-v3.5", DocumentsType: "text",
		Ranking: []types.RerankItem{{Index: 1, RelevanceScore: 0.9}, {Index: 0, RelevanceScore: 0.4}},
	})

	doRerankSpan := findSpan(rec, "ai.rerank.doRerank")
	if doRerankSpan == nil {
		t.Fatal(`expected a span literally named "ai.rerank.doRerank" (not "reranking rerank-v3.5")`)
	}
	if v, ok := attrValue(doRerankSpan, "ai.operationId"); !ok || v.(string) != "ai.rerank.doRerank" {
		t.Errorf("ai.operationId = %v (ok=%v), want ai.rerank.doRerank", v, ok)
	}
	if v, ok := attrValue(doRerankSpan, "ai.model.provider"); !ok || v.(string) != "cohere" {
		t.Errorf("expected ai.model.provider reused from the root span's base attrs, got %v (ok=%v)", v, ok)
	}
	if v, ok := attrValue(doRerankSpan, "ai.documents"); !ok {
		t.Error("expected ai.documents to be set")
	} else if got := v.([]string); len(got) != 2 || got[0] != `"doc1"` || got[1] != `"doc2"` {
		t.Errorf("ai.documents = %v, want [\"doc1\" \"doc2\"]", got)
	}
	if v, ok := attrValue(doRerankSpan, "ai.ranking.type"); !ok || v.(string) != "text" {
		t.Errorf("ai.ranking.type = %v (ok=%v), want text", v, ok)
	}
	if _, ok := attrValue(doRerankSpan, "ai.ranking"); !ok {
		t.Error("expected ai.ranking to be set")
	}
	for _, key := range []string{"gen_ai.operation.name", "gen_ai.system", "gen_ai.request.model"} {
		if _, ok := attrValue(doRerankSpan, key); ok {
			t.Errorf("ai.rerank.doRerank span should not carry %s (TS has no gen_ai.* here)", key)
		}
	}
}

// TestLegacyOpenTelemetryOnEndPerOperationShape covers H3 item 3: OnEnd
// dispatches on TelemetryFinishEvent.OperationType to reproduce TS's
// per-operation root-span shape — onRerankOperationEnd sets nothing beyond
// ending the span (not even ai.response.finishReason), onEmbedOperationEnd
// sets only ai.embedding(s), and onObjectOperationEnd sets ai.response.object
// instead of ai.response.text, with a reduced usage set.
func TestLegacyOpenTelemetryOnEndPerOperationShape(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-onend-shape-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordOutputs: true}

	// rerank: OnEnd sets nothing at all.
	rerankCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.rerank", Settings: settings})
	integration.OnEnd(rerankCtx, TelemetryFinishEvent{OperationType: "ai.rerank", Settings: settings, FinishReason: "stop"})
	rerankSpan := findSpan(rec, "ai.rerank")
	if rerankSpan == nil {
		t.Fatal("expected an ai.rerank span")
	}
	if _, ok := attrValue(rerankSpan, "ai.response.finishReason"); ok {
		t.Error("ai.rerank root span's OnEnd should not set ai.response.finishReason (TS onRerankOperationEnd sets nothing)")
	}

	// embed: OnEnd sets only ai.embedding, no usage.
	embedCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.embed", Settings: settings})
	integration.OnEnd(embedCtx, TelemetryFinishEvent{
		OperationType: "ai.embed", Settings: settings,
		Embedding: []float64{0.1, 0.2},
		Usage:     TelemetryUsage{TotalTokens: int64p(5)},
	})
	embedSpan := findSpan(rec, "ai.embed")
	if embedSpan == nil {
		t.Fatal("expected an ai.embed span")
	}
	if v, ok := attrValue(embedSpan, "ai.embedding"); !ok || v.(string) != `[0.1,0.2]` {
		t.Errorf("ai.embedding = %v (ok=%v), want [0.1,0.2]", v, ok)
	}
	if _, ok := attrValue(embedSpan, "ai.usage.totalTokens"); ok {
		t.Error("ai.embed root span's OnEnd should not carry usage (that lives on the nested doEmbed span only)")
	}

	// generateObject: OnEnd sets ai.response.object, not ai.response.text,
	// and the reduced (5-field) usage set.
	objCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateObject", Settings: settings})
	integration.OnEnd(objCtx, TelemetryFinishEvent{
		OperationType: "ai.generateObject", Settings: settings,
		FinishReason: "stop",
		Text:         `{"name":"Ann"}`,
		Object:       map[string]interface{}{"name": "Ann"},
		Usage:        TelemetryUsage{InputTokens: int64p(3)},
	})
	objSpan := findSpan(rec, "ai.generateObject")
	if objSpan == nil {
		t.Fatal("expected an ai.generateObject span")
	}
	if v, ok := attrValue(objSpan, "ai.response.object"); !ok || v.(string) != `{"name":"Ann"}` {
		t.Errorf("ai.response.object = %v (ok=%v), want {\"name\":\"Ann\"}", v, ok)
	}
	if _, ok := attrValue(objSpan, "ai.response.text"); ok {
		t.Error("ai.generateObject root span should not carry ai.response.text (that's the generateText shape)")
	}
	if v, ok := attrValue(objSpan, "ai.usage.inputTokens"); !ok || v.(int64) != 3 {
		t.Errorf("ai.usage.inputTokens = %v (ok=%v), want 3", v, ok)
	}

	// No gen_ai.usage.* dual-emission on any of the three root spans.
	for _, span := range []sdktrace.ReadOnlySpan{rerankSpan, embedSpan, objSpan} {
		for _, key := range []string{"gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens"} {
			if _, ok := attrValue(span, key); ok {
				t.Errorf("%s root span should not carry %s", span.Name(), key)
			}
		}
	}
}

// TestLegacyOpenTelemetryToolCallSpan ports TS's legacy-open-telemetry.test.ts
// "should record tool call telemetry data" case (see the matching
// __snapshots__ entry, which shows `"name": "ai.toolCall"` and
// `"ai.operationId": "ai.toolCall"` on the tool span, with no per-tool-name
// suffix anywhere): the real span name is the bare "ai.toolCall" (never
// suffixed with the tool name), it carries operation.name/ai.operationId
// (assembleOperationName), and ai.toolCall.args/result are JSON-encoded and
// gated by RecordOutputs (TS wraps both in an `output: () => ...`
// accessor), not by RecordInputs.
func TestLegacyOpenTelemetryToolCallSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-toolcall-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
	})
	toolCtx := integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{
		Settings:   settings,
		ToolCallID: "tool-call-1",
		ToolName:   "myTool",
		Args:       map[string]interface{}{"query": "test"},
	})
	integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{
		Settings:   settings,
		ToolCallID: "tool-call-1",
		ToolName:   "myTool",
		Result:     "result1",
	})

	toolSpan := findSpan(rec, "ai.toolCall")
	if toolSpan == nil {
		t.Fatal("expected the tool call span to be named the bare 'ai.toolCall' (not suffixed with the tool name)")
	}
	if v, ok := attrValue(toolSpan, "ai.operationId"); !ok || v.(string) != "ai.toolCall" {
		t.Errorf("ai.operationId = %v (ok=%v), want ai.toolCall", v, ok)
	}
	if v, ok := attrValue(toolSpan, "operation.name"); !ok || v.(string) != "ai.toolCall" {
		t.Errorf("operation.name = %v (ok=%v), want ai.toolCall", v, ok)
	}
	if v, ok := attrValue(toolSpan, "ai.toolCall.name"); !ok || v.(string) != "myTool" {
		t.Errorf("ai.toolCall.name = %v (ok=%v), want myTool", v, ok)
	}
	if v, ok := attrValue(toolSpan, "ai.toolCall.id"); !ok || v.(string) != "tool-call-1" {
		t.Errorf("ai.toolCall.id = %v (ok=%v), want tool-call-1", v, ok)
	}
	if v, ok := attrValue(toolSpan, "ai.toolCall.args"); !ok || v.(string) != `{"query":"test"}` {
		t.Errorf("ai.toolCall.args = %v (ok=%v), want {\"query\":\"test\"}", v, ok)
	}
	if v, ok := attrValue(toolSpan, "ai.toolCall.result"); !ok || v.(string) != `"result1"` {
		t.Errorf(`ai.toolCall.result = %v (ok=%v), want "result1"`, v, ok)
	}
}

// TestLegacyOpenTelemetryToolCallSpan_RecordOutputsFalse verifies
// ai.toolCall.args/result are absent (not just recomputed) when
// RecordOutputs is false, matching TS's output()-accessor gating.
func TestLegacyOpenTelemetryToolCallSpan_RecordOutputsFalse(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-toolcall-recordoutputs-false-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: false}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
	})
	toolCtx := integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{
		Settings:   settings,
		ToolCallID: "tool-call-1",
		ToolName:   "myTool",
		Args:       map[string]interface{}{"query": "test"},
	})
	integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{
		Settings:   settings,
		ToolCallID: "tool-call-1",
		ToolName:   "myTool",
		Result:     "result1",
	})

	toolSpan := findSpan(rec, "ai.toolCall")
	if toolSpan == nil {
		t.Fatal("expected an 'ai.toolCall' span")
	}
	if _, ok := attrValue(toolSpan, "ai.toolCall.args"); ok {
		t.Error("expected ai.toolCall.args to be absent when RecordOutputs is false")
	}
	if _, ok := attrValue(toolSpan, "ai.toolCall.result"); ok {
		t.Error("expected ai.toolCall.result to be absent when RecordOutputs is false")
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
	// No OnLanguageModelCallStart/End here (H4 item 1): LegacyOpenTelemetry no
	// longer implements those methods — TS's LegacyOpenTelemetry never
	// creates a "chat"/languageModel span at all, only the doGenerate/
	// doStream step span (OnStepStart/OnStepEnd above/below).
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
	if len(ended) != 5 {
		t.Fatalf("ended spans = %d, want 5", len(ended))
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
	// No "languageModel" span type here (H4 item 1): LegacyOpenTelemetry no
	// longer creates a nested model-call/"chat" span — only GenAI's
	// OpenTelemetry does.
	for _, spanType := range []string{"operation", "step", "tool", "embedding", "reranking"} {
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
	if spansByType["embedding"]["custom.call_id"] != "embed-1" || spansByType["reranking"]["custom.call_id"] != "rerank-1" {
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
		if span.Name() == "ai.toolCall" {
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

// TestLegacyOpenTelemetryStreamFirstChunkFinishEvents covers H3 item 4(a):
// TS's onStepEnd (legacy-open-telemetry.ts) adds "ai.stream.firstChunk" /
// "ai.stream.finish" span events (plus the matching ai.response.msToFirstChunk/
// msToFinish/avgOutputTokensPerSecond attributes), gated on isStreamText —
// see the legacy-open-telemetry.test.ts streamText integration snapshot
// (events: [{"name": "ai.stream.firstChunk", ...}, {"name": "ai.stream.finish", ...}]).
// ai.generateText (non-streaming) must NOT get either the attributes or the
// events, even when Performance data happens to be set.
func TestLegacyOpenTelemetryStreamFirstChunkFinishEvents(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-stream-events-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}
	perf := LanguageModelCallPerformance{
		ResponseTimeMs:                 500,
		EffectiveOutputTokensPerSecond: 20,
		TimeToFirstOutputMs:            int64p(100),
	}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.streamText", Settings: settings})
	stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
		OperationType: "ai.streamText", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})
	integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{
		OperationType: "ai.streamText", Settings: settings, FinishReason: "stop", Performance: perf,
	})

	stepSpan := findSpan(rec, "ai.streamText.doStream")
	if stepSpan == nil {
		t.Fatal("expected an ai.streamText.doStream step span")
	}
	if v, ok := attrValue(stepSpan, "ai.response.msToFirstChunk"); !ok || v.(int64) != 100 {
		t.Errorf("ai.response.msToFirstChunk = %v (ok=%v), want 100", v, ok)
	}
	if v, ok := attrValue(stepSpan, "ai.response.msToFinish"); !ok || v.(int64) != 500 {
		t.Errorf("ai.response.msToFinish = %v (ok=%v), want 500", v, ok)
	}
	if v, ok := attrValue(stepSpan, "ai.response.avgOutputTokensPerSecond"); !ok || v.(float64) != 20 {
		t.Errorf("ai.response.avgOutputTokensPerSecond = %v (ok=%v), want 20", v, ok)
	}
	events := stepSpan.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 span events, got %d: %+v", len(events), events)
	}
	if events[0].Name != "ai.stream.firstChunk" {
		t.Errorf("events[0].Name = %q, want ai.stream.firstChunk", events[0].Name)
	}
	if events[1].Name != "ai.stream.finish" {
		t.Errorf("events[1].Name = %q, want ai.stream.finish", events[1].Name)
	}

	// ai.generateText (non-streaming) must not get these attributes/events.
	rootCtx2 := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	stepCtx2 := integration.OnStepStart(rootCtx2, TelemetryStepStartEvent{
		OperationType: "ai.generateText", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})
	integration.OnStepEnd(stepCtx2, TelemetryStepEndEvent{
		OperationType: "ai.generateText", Settings: settings, FinishReason: "stop", Performance: perf,
	})
	genStepSpan := findSpan(rec, "ai.generateText.doGenerate")
	if genStepSpan == nil {
		t.Fatal("expected an ai.generateText.doGenerate step span")
	}
	if _, ok := attrValue(genStepSpan, "ai.response.msToFirstChunk"); ok {
		t.Error("ai.generateText step span should not carry ai.response.msToFirstChunk")
	}
	if len(genStepSpan.Events()) != 0 {
		t.Errorf("ai.generateText step span should have no events, got %+v", genStepSpan.Events())
	}
}

// TestLegacyOpenTelemetryObjectStepFirstChunkEvent covers H3 item 4(a) for
// the deprecated onObjectStepEnd shape: TS sets ai.stream.msToFirstChunk
// (not ai.response.msToFirstChunk) directly and adds an "ai.stream.firstChunk"
// event, only when msToFirstChunk is non-nil (never for non-streaming
// generateObject).
func TestLegacyOpenTelemetryObjectStepFirstChunkEvent(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("legacy-object-step-events-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.streamObject", Settings: settings})
	stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
		OperationType: "ai.streamObject", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})
	integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{
		OperationType: "ai.streamObject", Settings: settings, FinishReason: "stop",
		Performance: LanguageModelCallPerformance{TimeToFirstOutputMs: int64p(75)},
	})

	stepSpan := findSpan(rec, "ai.streamObject.doStream")
	if stepSpan == nil {
		t.Fatal("expected an ai.streamObject.doStream step span")
	}
	if v, ok := attrValue(stepSpan, "ai.stream.msToFirstChunk"); !ok || v.(int64) != 75 {
		t.Errorf("ai.stream.msToFirstChunk = %v (ok=%v), want 75", v, ok)
	}
	events := stepSpan.Events()
	if len(events) != 1 || events[0].Name != "ai.stream.firstChunk" {
		t.Fatalf("expected exactly one ai.stream.firstChunk event, got %+v", events)
	}

	// Non-streaming generateObject: no Performance data, so no event/attribute.
	rootCtx2 := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateObject", Settings: settings})
	stepCtx2 := integration.OnStepStart(rootCtx2, TelemetryStepStartEvent{
		OperationType: "ai.generateObject", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})
	integration.OnStepEnd(stepCtx2, TelemetryStepEndEvent{
		OperationType: "ai.generateObject", Settings: settings, FinishReason: "stop",
	})
	genStepSpan := findSpan(rec, "ai.generateObject.doGenerate")
	if genStepSpan == nil {
		t.Fatal("expected an ai.generateObject.doGenerate step span")
	}
	if _, ok := attrValue(genStepSpan, "ai.stream.msToFirstChunk"); ok {
		t.Error("non-streaming ai.generateObject step span should not carry ai.stream.msToFirstChunk")
	}
	if len(genStepSpan.Events()) != 0 {
		t.Errorf("non-streaming ai.generateObject step span should have no events, got %+v", genStepSpan.Events())
	}
}

// TestLegacyOpenTelemetrySpanStatusErrorOnErrorFinishReason ports TS
// 51e1763d2b (#21915) for LegacyOpenTelemetry: the step span (OnStepEnd) and
// root span (OnEnd) must get SpanStatusCode.ERROR when the corresponding
// finish event's FinishReason is "error", and otherwise keep the default
// Unset status.
func TestLegacyOpenTelemetrySpanStatusErrorOnErrorFinishReason(t *testing.T) {
	for _, finishReason := range []string{"error", "stop"} {
		t.Run(finishReason, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			tracer := tp.Tracer("legacy-finish-status-test")

			integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
			settings := &Settings{IsEnabled: Bool(true)}

			rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{
				OperationType: "ai.generateText", Settings: settings, CallID: "call-1",
			})
			stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
				OperationType: "ai.generateText", Settings: settings, CallID: "call-1",
			})
			integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{
				OperationType: "ai.generateText", Settings: settings, CallID: "call-1", FinishReason: finishReason,
			})
			integration.OnEnd(rootCtx, TelemetryFinishEvent{
				OperationType: "ai.generateText", Settings: settings, CallID: "call-1", FinishReason: finishReason,
			})

			wantCode := codes.Unset
			if finishReason == "error" {
				wantCode = codes.Error
			}
			stepSpan := findSpan(rec, "ai.generateText.doGenerate")
			if stepSpan == nil {
				t.Fatal("expected an ai.generateText.doGenerate step span")
			}
			if got := stepSpan.Status().Code; got != wantCode {
				t.Errorf("step span status = %v, want %v", got, wantCode)
			}
			rootSpan := findSpan(rec, "ai.generateText")
			if rootSpan == nil {
				t.Fatal("expected an ai.generateText root span")
			}
			if got := rootSpan.Status().Code; got != wantCode {
				t.Errorf("root span status = %v, want %v", got, wantCode)
			}
		})
	}
}
