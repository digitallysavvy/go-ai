package telemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r79carryIntegration is the subset of both OpenTelemetry integrations the
// span end order tests drive.
type r79carryIntegration interface {
	OnStart(context.Context, TelemetryStartEvent) context.Context
	OnStepStart(context.Context, TelemetryStepStartEvent) context.Context
	OnToolExecutionStart(context.Context, TelemetryToolCallStartEvent) context.Context
	OnEmbedStart(context.Context, EmbeddingModelCallStartEvent)
	OnError(context.Context, TelemetryErrorEvent)
	OnAbort(context.Context, TelemetryAbortEvent)
}

const r79carryToolCount = 8

// r79carryStartTextCall opens a generateText root, a step, a chat span (GenAI
// only) and r79carryToolCount tool spans, returning the span IDs in the order
// TS onAbort/onError end them: tool spans in start order, the chat span, the
// step span, the root.
func r79carryStartTextCall(t *testing.T, rec *tracetest.SpanRecorder, it r79carryIntegration, callID string) []trace.SpanID {
	t.Helper()
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateText", ModelID: "m", Settings: settings})
	stepCtx := it.OnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, OperationType: "ai.generateText", ModelID: "m", Settings: settings})
	if genAI, ok := it.(OpenTelemetry); ok {
		genAI.OnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{CallID: callID, ModelID: "m", Settings: settings})
	}
	// Tool call IDs in reverse lexical order, so neither a map nor a sort
	// yields start order by accident.
	for n := range r79carryToolCount {
		it.OnToolExecutionStart(stepCtx, TelemetryToolCallStartEvent{
			CallID: callID, ToolCallID: fmt.Sprintf("call-%d", r79carryToolCount-n), ToolName: fmt.Sprintf("tool%d", n), Settings: settings,
		})
	}
	started := rec.Started()
	var root, step, chat trace.SpanID
	var tools []trace.SpanID
	for _, s := range started {
		switch s.Name() {
		case "invoke_agent m", "ai.generateText":
			root = s.SpanContext().SpanID()
		case "step 1", "ai.generateText.doGenerate":
			step = s.SpanContext().SpanID()
		case "chat m":
			chat = s.SpanContext().SpanID()
		default:
			tools = append(tools, s.SpanContext().SpanID())
		}
	}
	if len(tools) != r79carryToolCount {
		t.Fatalf("started %d tool spans, want %d", len(tools), r79carryToolCount)
	}
	want := append([]trace.SpanID{}, tools...)
	if chat.IsValid() {
		want = append(want, chat)
	}
	return append(want, step, root)
}

// r79carryStartEmbedCall opens an embedMany root with r79carryToolCount
// embedding spans, returning their IDs in start order followed by the root.
func r79carryStartEmbedCall(t *testing.T, rec *tracetest.SpanRecorder, it r79carryIntegration, callID string) []trace.SpanID {
	t.Helper()
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.embedMany", ModelID: "m", Settings: settings})
	for n := range r79carryToolCount {
		it.OnEmbedStart(ctx, EmbeddingModelCallStartEvent{
			CallID: callID, EmbedCallID: fmt.Sprintf("embed-%d", r79carryToolCount-n), OperationID: "ai.embedMany.doEmbed", ModelID: "m", Settings: settings, Values: []string{"v"},
		})
	}
	started := rec.Started()
	want := make([]trace.SpanID, 0, len(started))
	for _, s := range started[1:] {
		want = append(want, s.SpanContext().SpanID())
	}
	return append(want, started[0].SpanContext().SpanID())
}

func r79carryAssertEndOrder(t *testing.T, rec *tracetest.SpanRecorder, want []trace.SpanID) {
	t.Helper()
	ended := rec.Ended()
	if len(ended) != len(want) {
		t.Fatalf("ended %d spans, want %d", len(ended), len(want))
	}
	for n, s := range ended {
		if s.SpanContext().SpanID() != want[n] {
			var names []string
			for _, e := range ended {
				names = append(names, e.Name())
			}
			t.Fatalf("span %d ended out of TS order; ended: %q", n, names)
		}
	}
}

// Round 79 carry: TS onAbort/onError end a call's open child spans in a fixed
// order, the Map-held tool and embedding spans in insertion order (GenAI:
// tools, chat, step, embeddings, rerank, decision, root; legacy: tools, step,
// embeddings, rerank, decision, root). Go ended the step span before the tool
// spans inside it, and iterated Go maps, so parallel tool or embedding spans
// ended in random order.
func TestR79Carry_AbortAndErrorEndOpenSpansInTSOrder(t *testing.T) {
	integrations := map[string]func(trace.Tracer) r79carryIntegration{
		"genai": func(tr trace.Tracer) r79carryIntegration { return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tr}) },
		"legacy": func(tr trace.Tracer) r79carryIntegration {
			return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tr})
		},
	}
	for name, build := range integrations {
		for _, terminal := range []string{"abort", "error"} {
			t.Run(name+"/"+terminal+"/text", func(t *testing.T) {
				rec := tracetest.NewSpanRecorder()
				it := build(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry"))
				callID := "r79carry-text-" + name + "-" + terminal
				want := r79carryStartTextCall(t, rec, it, callID)
				if terminal == "abort" {
					it.OnAbort(context.Background(), TelemetryAbortEvent{CallID: callID})
				} else {
					it.OnError(context.Background(), TelemetryErrorEvent{CallID: callID, Error: errors.New("boom")})
				}
				r79carryAssertEndOrder(t, rec, want)
			})
			t.Run(name+"/"+terminal+"/embedMany", func(t *testing.T) {
				rec := tracetest.NewSpanRecorder()
				it := build(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry"))
				callID := "r79carry-embed-" + name + "-" + terminal
				want := r79carryStartEmbedCall(t, rec, it, callID)
				if terminal == "abort" {
					it.OnAbort(context.Background(), TelemetryAbortEvent{CallID: callID})
				} else {
					it.OnError(context.Background(), TelemetryErrorEvent{CallID: callID, Error: errors.New("boom")})
				}
				r79carryAssertEndOrder(t, rec, want)
			})
		}
	}
}

// spanSet keeps Map semantics: re-setting a key keeps its position, take
// removes it, drain empties the set.
func TestR79Carry_SpanSetKeepsInsertionOrder(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tr := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry")
	span := func(name string) trace.Span {
		_, s := tr.Start(context.Background(), name)
		return s
	}
	var s spanSet
	a, b, c, a2 := span("a"), span("b"), span("c"), span("a2")
	s.set("a", a)
	s.set("b", b)
	s.set("c", c)
	s.set("a", a2)
	if got := s.take("b"); got != b {
		t.Fatalf("take(b) = %v, want b", got)
	}
	if got := s.take("missing"); got != nil {
		t.Fatalf("take(missing) = %v, want nil", got)
	}
	got := s.drain()
	if len(got) != 2 || got[0] != a2 || got[1] != c {
		t.Fatalf("drain = %v, want [a2 c]", got)
	}
	if rest := s.drain(); len(rest) != 0 {
		t.Fatalf("second drain = %v, want empty", rest)
	}
}

// TS open-telemetry.test.ts "keeps deferred provider tool calls visible
// before their result arrives": a provider-executed call without a result
// gets its execute_tool span (no result) in step 1; the result arriving in
// step 2 opens no second tool span and is reported as a tool_call_response
// in the second chat span's output messages.
func TestR79Carry_DeferredProviderToolCallVisibleBeforeResult(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	ot := NewOpenTelemetry(OpenTelemetryOptions{Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("r79carry")})
	settings := &Settings{IsEnabled: Bool(true)}
	const callID = "r79carry-deferred"
	ctx := ot.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateText", ModelID: "m", Settings: settings})

	step1 := ot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, ModelID: "m", Settings: settings})
	call1 := ot.OnLanguageModelCallStart(step1, LanguageModelCallStartEvent{CallID: callID, ModelID: "m", Settings: settings})
	ot.OnLanguageModelCallEnd(call1, LanguageModelCallEndEvent{CallID: callID, ModelID: "m", Settings: settings, FinishReason: "stop",
		Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "tc1", ToolName: "code_execution", Input: `{"code":"1 + 1"}`, ProviderExecuted: true}},
	})
	tools := findSpans(rec, "execute_tool code_execution")
	if len(tools) != 1 {
		t.Fatalf("execute_tool spans after step 1 = %d, want 1", len(tools))
	}
	if tools[0].EndTime().IsZero() {
		t.Fatal("execute_tool span not ended")
	}
	if _, ok := attrValue(tools[0], "gen_ai.tool.call.result"); ok {
		t.Fatal("deferred call recorded a gen_ai.tool.call.result")
	}
	ot.OnStepEnd(step1, TelemetryStepEndEvent{CallID: callID, Settings: settings, FinishReason: "tool-calls"})

	step2 := ot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, ModelID: "m", StepNumber: 1, Settings: settings})
	call2 := ot.OnLanguageModelCallStart(step2, LanguageModelCallStartEvent{CallID: callID, ModelID: "m", Settings: settings})
	ot.OnLanguageModelCallEnd(call2, LanguageModelCallEndEvent{CallID: callID, ModelID: "m", Settings: settings, FinishReason: "stop",
		Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "tc1", ToolName: "code_execution", Input: map[string]interface{}{"code": "1 + 1"}, Result: map[string]interface{}{"value": 2}, ProviderExecuted: true}},
	})
	if n := len(findSpans(rec, "execute_tool code_execution")); n != 1 {
		t.Fatalf("execute_tool spans after step 2 = %d, want 1", n)
	}
	chats := findSpans(rec, "chat m")
	if len(chats) != 2 {
		t.Fatalf("chat spans = %d, want 2", len(chats))
	}
	v, _ := attrValue(chats[1], "gen_ai.output.messages")
	const want = `[{"role":"assistant","parts":[{"type":"tool_call_response","id":"tc1","response":{"value":2}}],"finish_reason":"stop"}]`
	if v != want {
		t.Fatalf("step 2 gen_ai.output.messages = %v\nwant %s", v, want)
	}
}
