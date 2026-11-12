package telemetry

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// GetBaseAttributes leaves out credential headers whatever their case.
func TestR92carryGetBaseAttributesSkipsCredentialHeadersCaseInsensitively(t *testing.T) {
	attrs := GetBaseAttributes("p", "m", nil, map[string]string{
		"authorization": "Bearer s1", "AUTHORIZATION": "Bearer s2", "X-Api-Key": "s3", "Api-Key": "s4", "x-trace": "t",
	})
	for _, kv := range attrs {
		switch string(kv.Key) {
		case "ai.model.provider", "ai.model.id", "ai.request.headers.x-trace":
		default:
			t.Fatalf("unexpected attribute %s = %s", kv.Key, kv.Value.String())
		}
	}
	if len(attrs) != 3 {
		t.Fatalf("attrs = %v", attrs)
	}
}

// TS records toolCall.input as parsed from the model text, in its key order
// (ai.response.toolCalls, gen_ai tool_call arguments, gen_ai.tool.call.arguments).
func TestR92carryToolCallInputKeepsModelKeyOrder(t *testing.T) {
	tc := types.ToolCall{ID: "c1", ToolName: "w", RawArguments: `{"zeta":1,"alpha":{"y":2,"x":3}}`,
		Arguments: map[string]interface{}{"zeta": 1.0, "alpha": map[string]interface{}{"y": 2.0, "x": 3.0}}}
	got, ok := legacyToolCallsJSON([]types.ToolCall{tc})
	if want := `[{"toolCallId":"c1","toolName":"w","input":{"zeta":1,"alpha":{"y":2,"x":3}}}]`; !ok || got != want {
		t.Fatalf("ai.response.toolCalls = %s\nwant %s", got, want)
	}
	b, err := jsonstr.Marshal(FormatOutputMessages(FormatOutputMessagesOptions{ToolCalls: []types.ToolCall{tc}, FinishReason: "tool-calls"}))
	if err != nil || !strings.Contains(string(b), `"arguments":{"zeta":1,"alpha":{"y":2,"x":3}}`) {
		t.Fatalf("gen_ai.output.messages = %s", b)
	}
	args, ok := toolCallArgumentsJSON(types.ToolCallContent{ToolCallID: "c1", ToolName: "w", Input: tc.RawArguments, Arguments: tc.Arguments})
	if !ok || args != `{"zeta":1,"alpha":{"y":2,"x":3}}` {
		t.Fatalf("gen_ai.tool.call.arguments = %s", args)
	}
	// Refined values win; the raw text only gives the order.
	refined := types.ToolCall{ID: "c2", ToolName: "w", RawArguments: `{"b":"raw","a":1}`, Arguments: map[string]interface{}{"a": 1.0, "b": "refined"}}
	if got, _ := legacyToolCallsJSON([]types.ToolCall{refined}); !strings.Contains(got, `"input":{"b":"refined","a":1}`) {
		t.Fatalf("refined = %s", got)
	}
	// An input that is not JSON stays the raw text.
	if got, _ := legacyToolCallsJSON([]types.ToolCall{{ID: "c3", ToolName: "w", RawArguments: "{bad"}}); !strings.Contains(got, `"input":"{bad"`) {
		t.Fatalf("invalid = %s", got)
	}
}

// Local tool spans record the tool input in the model's key order
// (ai.toolCall.args, gen_ai.tool.call.arguments); FilterEvent clears ArgsText
// with the other inputs.
func TestR92carryToolSpanArgsKeepModelKeyOrder(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		var integ TelemetryIntegration
		key := "gen_ai.tool.call.arguments"
		if legacy {
			integ = NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
			key = "ai.toolCall.args"
		} else {
			integ = NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}
		set := &Settings{Integrations: []TelemetryIntegration{integ}}
		id := t.Name() + map[bool]string{true: "l", false: "g"}[legacy]
		ctx := FireOnStart(context.Background(), TelemetryStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", ModelProvider: "p"})
		ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", ModelProvider: "p"})
		tctx := FireOnToolCallStart(ctx, TelemetryToolCallStartEvent{Settings: set, CallID: id, ToolCallID: "c1", ToolName: "w",
			Args: map[string]interface{}{"zeta": 1, "alpha": 2}, ArgsText: `{"zeta":1,"alpha":2}`})
		FireOnToolCallFinish(tctx, TelemetryToolCallFinishEvent{Settings: set, CallID: id, ToolCallID: "c1", ToolName: "w", Result: "ok"})
		FireOnStepEnd(ctx, TelemetryStepEndEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
		FireOnEnd(ctx, TelemetryFinishEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
		_ = tp.Shutdown(context.Background())
		found := false
		for _, s := range rec.Ended() {
			for _, kv := range s.Attributes() {
				if string(kv.Key) == key {
					found = true
					if got := kv.Value.AsString(); got != `{"zeta":1,"alpha":2}` {
						t.Fatalf("legacy=%v %s = %s", legacy, key, got)
					}
				}
			}
		}
		if !found {
			t.Fatalf("legacy=%v: no %s", legacy, key)
		}
	}
	hidden := FilterEvent(TelemetryToolCallStartEvent{ArgsText: `{"a":1}`, Args: map[string]interface{}{"a": 1}}, &Options{RecordInputs: Bool(false)})
	if hidden.ArgsText != "" || hidden.Args != nil {
		t.Fatalf("filtered = %+v", hidden)
	}
}
