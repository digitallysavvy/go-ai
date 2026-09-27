package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMapProviderName(t *testing.T) {
	cases := map[string]string{
		"openai.chat":              "openai",
		"anthropic.messages":       "anthropic",
		"google.vertex.chat":       "gcp.vertex_ai",
		"google.generative-ai":     "gcp.gemini",
		"google-vertex":            "gcp.vertex_ai",
		"google":                   "gcp.gemini",
		"amazon-bedrock.converse":  "aws.bedrock",
		"azure-openai.chat":        "azure.ai.openai",
		"azure.chat":               "azure.ai.inference",
		"mistral.chat":             "mistral_ai",
		"xai.chat":                 "x_ai",
		"totally-unknown-provider": "totally-unknown-provider",
	}
	for input, want := range cases {
		if got := mapProviderName(input); got != want {
			t.Errorf("mapProviderName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMapOperationName(t *testing.T) {
	cases := map[string]string{
		"ai.generateText":   "invoke_agent",
		"ai.streamText":     "invoke_agent",
		"ai.generateObject": "invoke_agent",
		"ai.streamObject":   "invoke_agent",
		"ai.embed":          "embeddings",
		"ai.embedMany":      "embeddings",
		"ai.rerank":         "rerank",
		"ai.something.else": "ai.something.else",
	}
	for input, want := range cases {
		if got := mapOperationName(input); got != want {
			t.Errorf("mapOperationName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOpenTelemetryOnStartAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), FunctionID: "my-fn"}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		ModelProvider: "openai.chat",
		ModelID:       "gpt-5",
		Settings:      settings,
	})
	trace.SpanFromContext(ctx).End()

	span := findSpan(rec, "ai.generateText my-fn")
	if span == nil {
		t.Fatal("expected a span named 'ai.generateText my-fn'")
	}
	if v, ok := attrValue(span, "gen_ai.operation.name"); !ok || v.(string) != "invoke_agent" {
		t.Fatalf("expected gen_ai.operation.name=invoke_agent, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.provider.name"); !ok || v.(string) != "openai" {
		t.Fatalf("expected gen_ai.provider.name=openai, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.agent.name"); !ok || v.(string) != "my-fn" {
		t.Fatalf("expected gen_ai.agent.name=my-fn, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.request.model"); !ok || v.(string) != "gpt-5" {
		t.Fatalf("expected gen_ai.request.model=gpt-5, got %v ok=%v", v, ok)
	}
}

func TestOpenTelemetryLanguageModelCallStartRequestParams(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	temp := 0.7
	maxTokens := 512
	seed := 42
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{
		Settings:        settings,
		CallID:          "call-1",
		ModelProvider:   "anthropic",
		ModelID:         "claude",
		Temperature:     &temp,
		MaxOutputTokens: &maxTokens,
		Seed:            &seed,
		StopSequences:   []string{"STOP"},
	})
	if !trace.SpanFromContext(ctx).IsRecording() {
		t.Fatal("expected the model-call ctx to carry a recording span")
	}
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "call-1", FinishReason: "stop"})

	span := findSpan(rec, "chat claude")
	if span == nil {
		t.Fatal("expected a 'chat claude' span")
	}
	if v, ok := attrValue(span, "gen_ai.request.temperature"); !ok || v.(float64) != 0.7 {
		t.Fatalf("expected gen_ai.request.temperature=0.7, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.request.max_tokens"); !ok || v.(int64) != 512 {
		t.Fatalf("expected gen_ai.request.max_tokens=512, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.request.seed"); !ok || v.(int64) != 42 {
		t.Fatalf("expected gen_ai.request.seed=42, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.response.finish_reasons"); !ok {
		t.Fatalf("expected gen_ai.response.finish_reasons to be set, ok=%v val=%v", ok, v)
	}
}

// TestOpenTelemetryEmbeddingUsageNotDoubleCounted covers c0a42bc: the root
// ai.embed span must not carry gen_ai.usage.input_tokens (only the
// embeddings request span does), so a trace-wide sum of that attribute
// counts the tokens once.
func TestOpenTelemetryEmbeddingUsageNotDoubleCounted(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.embed", Settings: settings})
	integration.OnEmbedStart(rootCtx, EmbeddingModelCallStartEvent{Settings: settings, CallID: "embed-1", ModelProvider: "openai", ModelID: "text-embedding-3"})
	integration.OnEmbedEnd(rootCtx, EmbeddingModelCallEndEvent{
		Settings: settings, CallID: "embed-1",
		Usage: types.EmbeddingUsage{InputTokens: 12},
	})
	total := int64(12)
	integration.OnEnd(rootCtx, TelemetryFinishEvent{
		Settings: settings,
		Usage:    TelemetryUsage{TotalTokens: &total},
	})

	rootSpan := findSpan(rec, "ai.embed")
	embedSpan := findSpan(rec, "embeddings text-embedding-3")
	if rootSpan == nil || embedSpan == nil {
		t.Fatalf("expected both spans, root=%v embed=%v", rootSpan, embedSpan)
	}
	if _, ok := attrValue(rootSpan, "gen_ai.usage.input_tokens"); ok {
		t.Fatal("expected the root ai.embed span to NOT carry gen_ai.usage.input_tokens (double count)")
	}
	if v, ok := attrValue(embedSpan, "gen_ai.usage.input_tokens"); !ok || v.(int64) != 12 {
		t.Fatalf("expected the embeddings span to carry gen_ai.usage.input_tokens=12, got %v ok=%v", v, ok)
	}
}

// TestOpenTelemetryProviderExecutedToolGetsExecuteToolSpan covers 5ad6abf: a
// provider-executed tool call surfaced in OnStepEnd gets its own
// execute_tool span with gen_ai.tool.type=extension.
func TestOpenTelemetryProviderExecutedToolGetsExecuteToolSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 0})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{
		Settings:     settings,
		StepNumber:   0,
		FinishReason: "tool-calls",
		ToolCalls: []types.ToolCall{
			{ID: "tc-1", ToolName: "web_search", ProviderExecuted: true},
		},
	})

	toolSpan := findSpan(rec, "execute_tool web_search")
	if toolSpan == nil {
		t.Fatal("expected an 'execute_tool web_search' span for the provider-executed tool call")
	}
	if v, ok := attrValue(toolSpan, "gen_ai.tool.type"); !ok || v.(string) != "extension" {
		t.Fatalf("expected gen_ai.tool.type=extension, got %v ok=%v", v, ok)
	}
}

func TestOpenTelemetryStepSpanNaming(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 2})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{Settings: settings, StepNumber: 2, FinishReason: "stop"})

	span := findSpan(rec, "step 2")
	if span == nil {
		t.Fatal("expected a span named 'step 2'")
	}
	if v, ok := attrValue(span, "gen_ai.operation.name"); !ok || v.(string) != "agent_step" {
		t.Fatalf("expected gen_ai.operation.name=agent_step, got %v ok=%v", v, ok)
	}
}
