package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// TS JSON.stringify writes the SemConv parts' keys in object literal order.
func TestR49SemConvPartKeyOrder(t *testing.T) {
	for _, c := range []struct {
		part semConvPart
		want string
	}{
		{semConvPart{"content": "a<b", "type": "text"}, `{"type":"text","content":"a<b"}`},
		{semConvPart{"arguments": map[string]interface{}{"q": 1}, "name": "n", "id": "c1", "type": "tool_call"}, `{"type":"tool_call","id":"c1","name":"n","arguments":{"q":1}}`},
		{semConvPart{"response": "r", "id": nil, "type": "tool_call_response"}, `{"type":"tool_call_response","id":null,"response":"r"}`},
		{semConvPart{"uri": "https://x", "mime_type": "image/png", "modality": "image", "type": "uri"}, `{"type":"uri","modality":"image","mime_type":"image/png","uri":"https://x"}`},
		{semConvPart{"content": "AA==", "mime_type": nil, "modality": "image", "type": "blob"}, `{"type":"blob","modality":"image","mime_type":null,"content":"AA=="}`},
		{semConvPart{"reason": "ok", "approved": true, "approval_id": "a", "type": "tool_approval_response"}, `{"type":"tool_approval_response","approval_id":"a","approved":true,"reason":"ok"}`},
		{semConvPart{"kind": "k", "type": "custom"}, `{"type":"custom","kind":"k"}`},
	} {
		b, err := jsonstr.Marshal(c.part)
		if err != nil || string(b) != c.want {
			t.Errorf("got %s (%v), want %s", b, err, c.want)
		}
	}
}

// The transcription output message is {role, parts} in TS order.
func TestR49TranscriptionOutputMessageKeyOrder(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.transcribe", CallID: "c", ModelID: "m"})
	integration.OnEnd(ctx, TelemetryFinishEvent{OperationType: "ai.transcribe", CallID: "c", Text: "hi"})
	ended := rec.Ended()
	if len(ended) != 1 {
		t.Fatalf("%d spans ended, want the root", len(ended))
	}
	got, _ := attrValue(ended[0], "gen_ai.output.messages")
	if want := `[{"role":"assistant","parts":[{"type":"text","content":"hi"}]}]`; got != want {
		t.Fatalf("gen_ai.output.messages = %v, want %s", got, want)
	}
}

// TS recordErrorOnSpan: a provider-executed tool error that is not an Error
// sets the error status without a message or exception event.
func TestR49ProviderToolErrorValueStatusHasNoMessage(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "c"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "c"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "c", ModelID: "m"})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings: settings, CallID: "c", FinishReason: "stop",
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "t1", ToolName: "web_search", ProviderExecuted: true},
			types.ToolErrorContent{ToolCallID: "t1", ToolName: "web_search", Error: map[string]interface{}{"code": "unavailable"}},
		},
	})
	spans := findSpans(rec, "execute_tool web_search")
	if len(spans) != 1 {
		t.Fatalf("%d tool spans", len(spans))
	}
	if st := spans[0].Status(); st.Code != codes.Error || st.Description != "" {
		t.Fatalf("status = %+v, want error with no message", st)
	}
	if n := len(spans[0].Events()); n != 0 {
		t.Fatalf("%d span events, want none", n)
	}
}

// TS onObjectStepEnd records ai.response.object for an empty object text too:
// JSON.parse('') throws, so the raw "" is recorded.
func TestR49LegacyObjectStepRecordsEmptyObjectText(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.streamObject", CallID: "o"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{OperationType: "ai.streamObject", CallID: "o"})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{OperationType: "ai.streamObject", CallID: "o", FinishReason: "other"})
	span := findSpan(rec, "ai.streamObject.doStream")
	if span == nil {
		t.Fatal("missing step span")
	}
	if v, ok := attrValue(span, "ai.response.object"); !ok || v != "" {
		t.Fatalf("ai.response.object = %v (ok=%v), want \"\"", v, ok)
	}
}
