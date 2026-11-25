package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS open-telemetry.ts records gen_ai.tool.call.arguments (recordInputs) and
// gen_ai.tool.call.result (recordOutputs) on execute_tool spans.
func TestOpenTelemetryExecuteToolSpanRecordsArgumentsAndResult(t *testing.T) {
	for _, tc := range []struct {
		name            string
		in, out         bool
		wantArgs, wantR bool
	}{
		{"both", true, true, true, true},
		{"inputs only", true, false, true, false},
		{"outputs only", false, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
			settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(tc.in), RecordOutputs: Bool(tc.out)}

			ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "c1"})
			ctx = integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{
				Settings: settings, CallID: "c1", ToolCallID: "t1", ToolName: "lookup",
				Args: map[string]interface{}{"city": "Paris"},
			})
			integration.OnToolExecutionEnd(ctx, TelemetryToolCallFinishEvent{
				Settings: settings, CallID: "c1", ToolCallID: "t1", ToolName: "lookup",
				Result: map[string]interface{}{"temp": 21},
			})

			span := findSpan(rec, "execute_tool lookup")
			if span == nil {
				t.Fatal("no execute_tool span")
			}
			args, hasArgs := attrValue(span, "gen_ai.tool.call.arguments")
			res, hasRes := attrValue(span, "gen_ai.tool.call.result")
			if hasArgs != tc.wantArgs || hasRes != tc.wantR {
				t.Fatalf("args=%v/%v result=%v/%v", args, hasArgs, res, hasRes)
			}
			if hasArgs && args.(string) != `{"city":"Paris"}` {
				t.Fatalf("args=%v", args)
			}
			if hasRes && res.(string) != `{"temp":21}` {
				t.Fatalf("result=%v", res)
			}
		})
	}
}

func TestOpenTelemetryProviderExecutedToolRecordsArguments(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(true), RecordOutputs: Bool(false)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "c2"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "c2"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "c2", ModelID: "m"})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings: settings, CallID: "c2", FinishReason: "tool-calls",
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "p1", ToolName: "web_search", ProviderExecuted: true, Input: `{ "q": "x" }`},
			types.ToolResultContent{ToolCallID: "p1", ToolName: "web_search", Result: "r"},
		},
	})
	span := findSpan(rec, "execute_tool web_search")
	if span == nil {
		t.Fatal("no span")
	}
	if v, ok := attrValue(span, "gen_ai.tool.call.arguments"); !ok || v.(string) != `{"q":"x"}` {
		t.Fatalf("arguments=%v ok=%v", v, ok)
	}
	if _, ok := attrValue(span, "gen_ai.tool.call.result"); ok {
		t.Fatal("result must be gated by recordOutputs")
	}
}
