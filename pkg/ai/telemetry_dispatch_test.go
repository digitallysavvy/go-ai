package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
		Telemetry: &telemetry.Options{
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
		Telemetry: &telemetry.Options{
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
		Telemetry: &telemetry.Options{
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
		Telemetry: &telemetry.Options{
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
		Telemetry: &telemetry.Options{
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
// telemetry integration's model-call span, so any spans the provider itself
// creates (e.g. for its HTTP request) are children of it instead of siblings
// of nothing. With only LegacyOpenTelemetry registered, that span is the
// "ai.generateText.doGenerate" step span itself (H4 item 1:
// LegacyOpenTelemetry no longer creates a separate nested "chat" span — TS's
// LegacyOpenTelemetry never did either, the step span IS the model-call
// span). See TestGenerateTextModelCallRunsInsideGenAIChatSpan below for
// GenAI's own "chat" span.
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
		Telemetry: &telemetry.Options{
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
	if !spanContextIsNamed(rec, sawCtx, "ai.generateText.doGenerate") {
		t.Fatal("expected the ctx passed to DoGenerate to carry the 'ai.generateText.doGenerate' step span")
	}
}

// spanContextIsNamed checks that ctx's span id matches a started span with
// exactly this name.
func spanContextIsNamed(rec *tracetest.SpanRecorder, ctx context.Context, name string) bool {
	seenCtxSpanID := otelapi.SpanContextFromContext(ctx).SpanID()
	for _, s := range rec.Started() {
		if s.Name() == name && s.SpanContext().SpanID() == seenCtxSpanID {
			return true
		}
	}
	return false
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

// TestGenerateTextModelCallRunsInsideGenAIChatSpan is the GenAI counterpart
// of TestGenerateTextModelCallRunsInsideChatSpan: with the GenAI integration
// registered, it still creates its own nested "chat <model>" span (only
// LegacyOpenTelemetry stopped doing so — H4 item 1), and DoGenerate must run
// inside it.
func TestGenerateTextModelCallRunsInsideGenAIChatSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("generate-genai-test")

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
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("GenerateText error: %v", err)
	}
	if sawCtx == nil {
		t.Fatal("expected DoGenerate to be called")
	}
	if !spanContextIsChatSpan(rec, sawCtx) {
		t.Fatal("expected the ctx passed to DoGenerate to carry the 'chat' span")
	}
}

// TestStreamTextModelCallRunsInsideChatSpan is the streaming counterpart of
// TestGenerateTextModelCallRunsInsideChatSpan. FireOnStepStart now fires
// BEFORE bootstrapAndStream's own DoStream call for step 1 (matching TS's
// sequential onStepStart-then-doStream ordering — the "ST" fix), so with
// only LegacyOpenTelemetry registered (which creates no separate nested
// "chat" span — H4 item 1, the step span IS the model-call span) DoStream
// runs inside the "ai.streamText.doStream" step span, not the bare root
// "ai.streamText" span.
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
		Telemetry: &telemetry.Options{
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
	if !spanContextIsNamed(rec, sawCtx, "ai.streamText.doStream") {
		t.Fatal("expected the ctx passed to DoStream to carry the 'ai.streamText.doStream' step span")
	}
}

// TestStreamTextModelCallRunsInsideGenAIChatSpan is the GenAI counterpart of
// TestStreamTextModelCallRunsInsideChatSpan: GenAI's OpenTelemetry still
// creates its own nested "chat <model>" span around the DoStream call
// regardless of step-span timing (only LegacyOpenTelemetry stopped doing so
// — H4 item 1).
func TestStreamTextModelCallRunsInsideGenAIChatSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-genai-test")

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
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
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

// TestStreamTextGenAIChatSpanNestsUnderStepSpanForFirstStep is the direct
// regression test for the "ST" ordering fix: FireOnStepStart now fires
// before step 1's DoStream call is issued (previously it only fired once
// processStream started consuming the stream DoStream had already returned),
// so GenAI's "chat" model-call span must nest directly under "step 1" for
// the very first step, not under the bare root "ai.streamText" span. This is
// also what let pkg/telemetry/open_telemetry.go's OnLanguageModelCallStart
// drop its root-span fallback (TS's onLanguageModelCallStart always resolves
// state.stepContext, with no root fallback).
func TestStreamTextGenAIChatSpanNestsUnderStepSpanForFirstStep(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-genai-nesting-test")

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hello",
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("stream ReadAll error: %v", err)
	}

	var chatSpan, stepSpan sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		switch {
		case strings.HasPrefix(s.Name(), "chat"):
			chatSpan = s
		case s.Name() == "step 1":
			stepSpan = s
		}
	}
	if chatSpan == nil {
		t.Fatal("expected a 'chat' span")
	}
	if stepSpan == nil {
		t.Fatal("expected a 'step 1' span")
	}
	if chatSpan.Parent().SpanID() != stepSpan.SpanContext().SpanID() {
		t.Fatalf("expected 'chat' span's parent to be 'step 1' (%s), got parent %s",
			stepSpan.SpanContext().SpanID(), chatSpan.Parent().SpanID())
	}
}

// TestStreamTextMultiStepEventOrdering asserts the full per-step event order
// across a multi-step stream (a tool call forcing a second step), mirroring
// TS's fully sequential streamStep() ordering in
// packages/ai/src/generate-text/stream-text.ts: for every step,
// onStepStart/telemetryDispatcher.onStepStart and
// onLanguageModelCallStart/telemetryDispatcher.onLanguageModelCallStart fire
// BEFORE that step's doStream call, and doStream itself is not issued until
// the previous step's tool execution and onStepEnd/onStepFinish have run
// (the "ST" fix: previously step 2's DoStream was issued eagerly at the tail
// of step 1's own iteration, but — critically for THIS test — always after
// step 1's own onStepStart/onLanguageModelCallStart, which already preceded
// step 1's DoStream; the bug was specific to telemetry.FireOnStepStart
// lagging behind DoStream, covered separately by
// TestStreamTextGenAIChatSpanNestsUnderStepSpanForFirstStep above).
func TestStreamTextMultiStepEventOrdering(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-multistep-order-test")

	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	tool := types.Tool{
		Name: "get_weather",
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			record("toolExecute")
			return "sunny", nil
		},
	}

	var doStreamCalls int32
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			n := atomic.AddInt32(&doStreamCalls, 1)
			record(fmt.Sprintf("doStream:%d", n))
			if n == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
						ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{},
					}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "step2-text"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "weather?",
		Tools:  []types.Tool{tool},
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) {
			record(fmt.Sprintf("stepStart:%d", e.StepNumber))
		},
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) {
			record("lmCallStart")
		},
		OnStepEnd: func(context.Context, types.StepResult, interface{}) {
			record("stepEnd")
		},
	})
	if err != nil {
		t.Fatalf("StreamText error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()

	want := []string{
		"stepStart:0", "lmCallStart", "doStream:1",
		"toolExecute", "stepEnd",
		"stepStart:1", "lmCallStart", "doStream:2",
		"stepEnd",
	}
	if len(got) != len(want) {
		t.Fatalf("event order mismatch:\n got:  %v\n want: %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event order mismatch at index %d:\n got:  %v\n want: %v", i, got, want)
		}
	}

	// Cross-check with the telemetry span tree: each step's "chat" span must
	// nest under that step's own "step N" span (both steps, not just the
	// first — the ST fix applies to every step, not only step 1).
	stepSpans := map[string]sdktrace.ReadOnlySpan{}
	var chatSpans []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		switch {
		case strings.HasPrefix(s.Name(), "chat"):
			chatSpans = append(chatSpans, s)
		case s.Name() == "step 1" || s.Name() == "step 2":
			stepSpans[s.Name()] = s
		}
	}
	if len(chatSpans) != 2 {
		t.Fatalf("expected 2 'chat' spans (one per step), got %d", len(chatSpans))
	}
	if len(stepSpans) != 2 {
		t.Fatalf("expected 'step 1' and 'step 2' spans, got %v", stepSpans)
	}
	for _, chat := range chatSpans {
		matched := false
		for _, step := range stepSpans {
			if chat.Parent().SpanID() == step.SpanContext().SpanID() {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("chat span %q's parent %s did not match any step span", chat.Name(), chat.Parent().SpanID())
		}
	}
}
