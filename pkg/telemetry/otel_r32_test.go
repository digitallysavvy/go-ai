package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// countingTracer wraps a tracer and records every span name it starts, so a
// test can tell whether child spans came from the caller-supplied tracer.
type countingTracer struct {
	trace.Tracer
	names *[]string
}

func (c countingTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	*c.names = append(*c.names, name)
	return c.Tracer.Start(ctx, name, opts...)
}

// TS gives each GenAI span a SpanKind (root: INTERNAL for text/object, CLIENT
// for audio/embed/rerank; step and tool: INTERNAL; chat/embeddings: CLIENT) and
// creates every span from the integration's own tracer.
func TestGenAISpanKindsAndSharedTracer(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	var names []string
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: countingTracer{tp.Tracer("t"), &names}})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText", Settings: settings, CallID: "c1", ModelID: "m", ModelProvider: "p",
	})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "c1", Settings: settings})
	integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{CallID: "c1", ModelID: "m", Settings: settings})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{CallID: "c1", Settings: settings})
	tctx := integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{CallID: "c1", ToolCallID: "t1", ToolName: "tool", Settings: settings})
	integration.OnToolExecutionEnd(tctx, TelemetryToolCallFinishEvent{CallID: "c1", ToolCallID: "t1", ToolName: "tool", Settings: settings})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{CallID: "c1", Settings: settings})
	integration.OnEnd(ctx, TelemetryFinishEvent{CallID: "c1", OperationType: "ai.generateText", Settings: settings})

	want := map[string]trace.SpanKind{
		"invoke_agent m":    trace.SpanKindInternal,
		"step 1":            trace.SpanKindInternal,
		"chat m":            trace.SpanKindClient,
		"execute_tool tool": trace.SpanKindInternal,
	}
	got := map[string]trace.SpanKind{}
	for _, s := range rec.Ended() {
		got[s.Name()] = s.SpanKind()
	}
	for name, kind := range want {
		if k, ok := got[name]; !ok {
			t.Errorf("missing span %q (have %v)", name, got)
		} else if k != kind {
			t.Errorf("span %q kind = %v, want %v", name, k, kind)
		}
	}
	if len(names) != 4 {
		t.Errorf("custom tracer started %v, want all 4 spans", names)
	}

	for op, kind := range map[string]trace.SpanKind{
		"ai.embed": trace.SpanKindClient, "ai.rerank": trace.SpanKindClient,
		"ai.transcribe": trace.SpanKindClient, "ai.generateObject": trace.SpanKindInternal,
	} {
		before := len(rec.Started())
		c := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: op, Settings: settings, CallID: "k-" + op, ModelID: "m"})
		integration.OnEnd(c, TelemetryFinishEvent{CallID: "k-" + op, OperationType: op, Settings: settings})
		started := rec.Started()
		if len(started) != before+1 || started[before].SpanKind() != kind {
			t.Errorf("%s root span kind wrong: %v", op, started[before:])
		}
	}
}

// TS records the transcript for every transcription, even an empty one, and
// goes through recordErrorOnSpan (HTTP status code) for failed tool calls.
func TestGenAIEmptyTranscriptAndToolErrorStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.transcribe", Settings: settings, CallID: "a", ModelID: "m"})
	integration.OnEnd(ctx, TelemetryFinishEvent{CallID: "a", OperationType: "ai.transcribe", Settings: settings, Text: ""})
	s := findSpan(rec, "generate_content m")
	if s == nil {
		for _, sp := range rec.Ended() {
			s = sp
		}
	}
	if v, ok := attrValue(s, "ai.response.text"); !ok || v.(string) != "" {
		t.Errorf("ai.response.text = %v (ok=%v), want empty string recorded", v, ok)
	}

	ctx = integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "b", ModelID: "m"})
	tctx := integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{CallID: "b", ToolCallID: "t", ToolName: "tool", Settings: settings})
	integration.OnToolExecutionEnd(tctx, TelemetryToolCallFinishEvent{
		CallID: "b", ToolCallID: "t", ToolName: "tool", Settings: settings,
		Error: providererrors.NewProviderError("p", 503, "", "down", nil),
	})
	tool := findSpan(rec, "execute_tool tool")
	if v, ok := attrValue(tool, "http.response.status_code"); !ok || v.(int64) != 503 {
		t.Errorf("http.response.status_code = %v (ok=%v), want 503", v, ok)
	}
}

// TS puts gen_ai.agent.name on the generate/object/audio roots, not on the
// embed or rerank roots.
func TestGenAIAgentNameOnlyOnTextObjectAudioRoots(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true), FunctionID: "fn"}
	for op, want := range map[string]bool{
		"ai.generateText": true, "ai.streamObject": true, "ai.transcribe": true, "ai.generateSpeech": true,
		"ai.embed": false, "ai.embedMany": false, "ai.rerank": false,
	} {
		before := len(rec.Started())
		c := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: op, Settings: settings, CallID: "n-" + op, ModelID: "m"})
		integration.OnEnd(c, TelemetryFinishEvent{CallID: "n-" + op, OperationType: op, Settings: settings})
		_, ok := attrValue(rec.Started()[before], "gen_ai.agent.name")
		if ok != want {
			t.Errorf("%s: gen_ai.agent.name present = %v, want %v", op, ok, want)
		}
	}
}

// TS onAbort ends an open decision model-call span along with the root.
func TestGenAIAbortEndsDecisionModelCallSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := integration.OnDecideStart(context.Background(), DecideStartEvent{Settings: settings, CallID: "d", ModelID: "m"})
	integration.OnDecisionModelCallStart(ctx, DecisionModelCallStartEvent{Settings: settings, CallID: "d", ModelID: "m"})
	integration.OnAbort(ctx, TelemetryAbortEvent{Settings: settings, CallID: "d"})
	if n := len(rec.Ended()); n != 2 {
		t.Fatalf("ended spans = %d, want 2 (decision model call + root)", n)
	}
}
