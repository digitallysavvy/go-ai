package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TS open-telemetry.test.ts "agentSpanName" (#23835).
func TestOpenTelemetryAgentSpanName(t *testing.T) {
	custom := func(o AgentSpanNameOptions) string {
		if o.FunctionID == "" {
			return ""
		}
		return "my-app " + o.FunctionID
	}
	cases := []struct {
		name       string
		opts       OpenTelemetryOptions
		operation  string
		functionID string
		want       string
	}{
		{"default", OpenTelemetryOptions{}, "ai.generateText", "my-agent", "invoke_agent gpt-4"},
		{"modelId", OpenTelemetryOptions{AgentSpanName: AgentSpanNameModelID}, "ai.generateText", "my-agent", "invoke_agent gpt-4"},
		{"functionId", OpenTelemetryOptions{AgentSpanName: AgentSpanNameFunctionID}, "ai.generateText", "my-agent", "invoke_agent my-agent"},
		{"functionId missing", OpenTelemetryOptions{AgentSpanName: AgentSpanNameFunctionID}, "ai.generateText", "", "invoke_agent gpt-4"},
		{"functionId object", OpenTelemetryOptions{AgentSpanName: AgentSpanNameFunctionID}, "ai.generateObject", "my-agent", "invoke_agent my-agent"},
		{"callback", OpenTelemetryOptions{AgentSpanNameFunc: custom}, "ai.streamText", "my-agent", "my-app my-agent"},
		{"callback empty falls back", OpenTelemetryOptions{AgentSpanNameFunc: custom}, "ai.streamText", "", "invoke_agent gpt-4"},
		{"callback panic falls back", OpenTelemetryOptions{AgentSpanNameFunc: func(AgentSpanNameOptions) string { panic("boom") }}, "ai.generateText", "my-agent", "invoke_agent gpt-4"},
		{"embeddings keep default", OpenTelemetryOptions{AgentSpanName: AgentSpanNameFunctionID}, "ai.embed", "my-agent", "embeddings gpt-4"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			tc.opts.Tracer = tp.Tracer("genai-test")
			integration := NewOpenTelemetry(tc.opts)
			ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
				OperationType: tc.operation,
				ModelProvider: "openai.chat",
				ModelID:       "gpt-4",
				Settings:      &Settings{IsEnabled: Bool(true), FunctionID: tc.functionID},
			})
			trace.SpanFromContext(ctx).End()
			if findSpan(rec, tc.want) == nil {
				t.Fatalf("no span named %q", tc.want)
			}
			if tc.functionID != "" {
				span := findSpan(rec, tc.want)
				if v, ok := attrValue(span, "gen_ai.agent.name"); tc.operation != "ai.embed" && (!ok || v.(string) != tc.functionID) {
					t.Fatalf("gen_ai.agent.name = %v ok=%v", v, ok)
				}
			}
		})
	}
}
