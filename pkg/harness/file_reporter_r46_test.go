package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// TS step.content is always an array, so an empty step is written with
// "output": [] (only recordOutputs: false leaves it out), and a hand-built
// telemetry.Options that leaves RecordInputs / RecordOutputs nil records.
func TestR46FileReporterEmptyStepWritesEmptyOutputAndNilOptionsRecord(t *testing.T) {
	r, dir := newTestReporter(t, false)
	ctx := context.Background()
	st := &telemetry.Options{}
	start := reporterStart("call-1")
	start.Settings = st
	r.OnStart(ctx, start)
	r.OnStepStart(ctx, telemetry.TelemetryStepStartEvent{CallID: "call-1"})
	r.OnStepEnd(ctx, telemetry.TelemetryStepEndEvent{CallID: "call-1", Settings: st})
	r.OnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: "call-1"})
	var sawStart, sawStep bool
	for _, l := range readEventLines(t, dir) {
		switch l["kind"] {
		case "turn-start":
			sawStart = true
			if _, ok := l["input"]; !ok {
				t.Fatalf("turn-start has no input with nil RecordInputs: %v", l)
			}
		case "step-finish":
			sawStep = true
			out, ok := l["output"].([]any)
			if !ok || len(out) != 0 {
				t.Fatalf("step-finish output = %#v, want []", l["output"])
			}
		}
	}
	if !sawStart || !sawStep {
		t.Fatal("missing turn-start or step-finish")
	}
}
