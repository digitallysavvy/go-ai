package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// RecordInputs/RecordOutputs are *bool: nil records (TS omitted
// recordInputs/recordOutputs mean true), only an explicit false turns it off.
func TestShouldRecordInputsAndOutputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    *Options
		want bool
	}{
		{"nil options", nil, true},
		{"Options literal", &Options{}, true},
		{"explicit true", &Options{RecordInputs: Bool(true), RecordOutputs: Bool(true)}, true},
		{"explicit false", &Options{RecordInputs: Bool(false), RecordOutputs: Bool(false)}, false},
		{"DefaultSettings", DefaultSettings(), true},
		{"WithRecord false", DefaultSettings().WithRecordInputs(false).WithRecordOutputs(false), false},
		{"WithRecord true", (&Options{RecordInputs: Bool(false), RecordOutputs: Bool(false)}).WithRecordInputs(true).WithRecordOutputs(true), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.ShouldRecordInputs(); got != tc.want {
				t.Fatalf("ShouldRecordInputs = %v, want %v", got, tc.want)
			}
			if got := tc.s.ShouldRecordOutputs(); got != tc.want {
				t.Fatalf("ShouldRecordOutputs = %v, want %v", got, tc.want)
			}
		})
	}
}

// Through an integration: a Settings{} literal records the tool arguments
// and result, as TS does when recordInputs/recordOutputs are omitted; an
// explicit false keeps each out of the span.
func TestSettingsLiteralRecordsLikeTS(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		settings             *Settings
		wantArgs, wantResult bool
	}{
		{"literal", &Settings{}, true, true},
		{"nil", nil, true, true},
		{"explicit true", &Settings{RecordInputs: Bool(true), RecordOutputs: Bool(true)}, true, true},
		{"inputs false", &Settings{RecordInputs: Bool(false)}, false, true},
		{"outputs false", &Settings{RecordOutputs: Bool(false)}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
			ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: tc.settings, CallID: "c1"})
			ctx = integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{
				Settings: tc.settings, CallID: "c1", ToolCallID: "t1", ToolName: "lookup",
				Args: map[string]interface{}{"city": "Paris"},
			})
			integration.OnToolExecutionEnd(ctx, TelemetryToolCallFinishEvent{
				Settings: tc.settings, CallID: "c1", ToolCallID: "t1", ToolName: "lookup",
				Result: map[string]interface{}{"temp": 21},
			})
			span := findSpan(rec, "execute_tool lookup")
			if span == nil {
				t.Fatal("no execute_tool span")
			}
			_, hasArgs := attrValue(span, "gen_ai.tool.call.arguments")
			_, hasResult := attrValue(span, "gen_ai.tool.call.result")
			if hasArgs != tc.wantArgs || hasResult != tc.wantResult {
				t.Fatalf("arguments recorded = %v (want %v), result recorded = %v (want %v)", hasArgs, tc.wantArgs, hasResult, tc.wantResult)
			}
		})
	}
}
