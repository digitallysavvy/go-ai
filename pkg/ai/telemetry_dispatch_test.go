package ai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	otelapi "go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func countSpansNamed(rec *tracetest.SpanRecorder, name string) int {
	count := 0
	for _, s := range rec.Ended() {
		if s.Name() == name {
			count++
		}
	}
	return count
}

// TestEmbedNoSpanWithoutIntegration and its siblings cover G4 (5d0f18e,
// 118b953): core must not create OTel spans directly. With no telemetry
// integration registered, no span exists at all; with one registered,
// exactly one span is created per operation (previously Embed/EmbedMany
// created their own "ai.embed"/"ai.embedMany" span directly in addition to
// whatever a registered OTel integration created for the same operation).
func TestEmbedNoSpanWithoutIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{Embedding: []float64{0.1, 0.2}}, nil
		},
	}
	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hello",
		Telemetry: &telemetry.Settings{
			IsEnabled: telemetry.Bool(true),
			// Explicit Noop isolates this test from any globally registered
			// integration another test in this package may have left behind.
			// tracer is unused since Noop never creates spans.
			Integrations: []telemetry.TelemetryIntegration{telemetry.NoopTelemetryIntegration{}},
		},
	})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if got := countSpansNamed(rec, "ai.embed"); got != 0 {
		t.Fatalf("expected no ai.embed span without a registered integration, got %d", got)
	}
}

func TestEmbedExactlyOneSpanWithIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("embed-test")

	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{Embedding: []float64{0.1, 0.2}}, nil
		},
	}
	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hello",
		Telemetry: &telemetry.Settings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if got := countSpansNamed(rec, "ai.embed"); got != 1 {
		t.Fatalf("expected exactly 1 ai.embed span, got %d", got)
	}
}

func TestEmbedManyExactlyOneSpanWithIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("embed-test")

	model := &testutil.MockEmbeddingModel{
		DoEmbedManyFunc: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}, {0.2}}}, nil
		},
	}
	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:  model,
		Inputs: []string{"a", "b"},
		Telemetry: &telemetry.Settings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("EmbedMany error: %v", err)
	}
	if got := countSpansNamed(rec, "ai.embedMany"); got != 1 {
		t.Fatalf("expected exactly 1 ai.embedMany span, got %d", got)
	}
}

func TestGenerateObjectNoSpanWithoutIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"ok":true}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "give me an object",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"ok": map[string]interface{}{"type": "boolean"}},
		}),
		Telemetry: &telemetry.Settings{
			IsEnabled: telemetry.Bool(true),
			// tracer is unused since Noop never creates spans.
			Integrations: []telemetry.TelemetryIntegration{telemetry.NoopTelemetryIntegration{}},
		},
	})
	if err != nil {
		t.Fatalf("GenerateObject error: %v", err)
	}
	if got := countSpansNamed(rec, "ai.generateObject"); got != 0 {
		t.Fatalf("expected no ai.generateObject span without a registered integration, got %d", got)
	}
}

func TestGenerateObjectExactlyOneSpanWithIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("object-test")

	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"ok":true}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "give me an object",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"ok": map[string]interface{}{"type": "boolean"}},
		}),
		Telemetry: &telemetry.Settings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("GenerateObject error: %v", err)
	}
	if got := countSpansNamed(rec, "ai.generateObject"); got != 1 {
		t.Fatalf("expected exactly 1 ai.generateObject span, got %d", got)
	}
}

// TestGenerateTextModelCallRunsInsideChatSpan covers G4/594029e: the
// provider's DoGenerate call must run inside the ctx that embeds the
// telemetry integration's "chat" (model-call) span, so any spans the
// provider itself creates (e.g. for its HTTP request) are children of it
// instead of siblings of nothing.
func TestGenerateTextModelCallRunsInsideChatSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("generate-test")

	var sawCtx context.Context
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			sawCtx = ctx
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hello",
		Telemetry: &telemetry.Settings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText error: %v", err)
	}
	if sawCtx == nil {
		t.Fatal("expected DoGenerate to be called")
	}
	if !otelapi.SpanContextFromContext(sawCtx).IsValid() {
		t.Fatal("expected the ctx passed to DoGenerate to carry a valid span context")
	}
	if !spanContextIsChatSpan(rec, sawCtx) {
		t.Fatal("expected the ctx passed to DoGenerate to carry the 'chat' span")
	}
}

// spanContextIsChatSpan checks that ctx's span id matches any started span
// whose name starts with "chat" (the model id suffix varies by mock model).
func spanContextIsChatSpan(rec *tracetest.SpanRecorder, ctx context.Context) bool {
	seenCtxSpanID := otelapi.SpanContextFromContext(ctx).SpanID()
	for _, s := range rec.Started() {
		if len(s.Name()) >= 4 && s.Name()[:4] == "chat" && s.SpanContext().SpanID() == seenCtxSpanID {
			return true
		}
	}
	return false
}

// TestStreamTextModelCallRunsInsideChatSpan is the streaming counterpart of
// TestGenerateTextModelCallRunsInsideChatSpan.
func TestStreamTextModelCallRunsInsideChatSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-test")

	var sawCtx context.Context
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			sawCtx = ctx
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hello",
		Telemetry: &telemetry.Settings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("stream ReadAll error: %v", err)
	}
	if sawCtx == nil {
		t.Fatal("expected DoStream to be called")
	}
	if !spanContextIsChatSpan(rec, sawCtx) {
		t.Fatal("expected the ctx passed to DoStream to carry the 'chat' span")
	}
}
