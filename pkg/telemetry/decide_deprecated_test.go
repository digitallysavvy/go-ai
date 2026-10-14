//nolint:staticcheck // exercises the deprecated aliases on purpose
package telemetry

import (
	"context"
	"testing"
)

type decideRecorder struct {
	NoopTelemetryIntegration
	calls []string
}

func (r *decideRecorder) OnDecideStart(ctx context.Context, _ DecideStartEvent) context.Context {
	r.calls = append(r.calls, "start")
	return ctx
}
func (r *decideRecorder) OnDecideEnd(context.Context, DecideEndEvent) { r.calls = append(r.calls, "end") }
func (r *decideRecorder) OnDecisionModelCallStart(context.Context, DecisionModelCallStartEvent) {
	r.calls = append(r.calls, "mstart")
}
func (r *decideRecorder) OnDecisionModelCallEnd(context.Context, DecisionModelCallEndEvent) {
	r.calls = append(r.calls, "mend")
}

func TestDeprecatedFireOnEvaluateWrappersReachDecideHandlers(t *testing.T) {
	rec := &decideRecorder{}
	settings := &Settings{IsEnabled: Bool(true), Integrations: []TelemetryIntegration{rec}}
	ctx := context.Background()
	ctx = FireOnEvaluateStart(ctx, EvaluateStartEvent{Settings: settings})
	FireOnEvaluationModelCallStart(ctx, EvaluationModelCallStartEvent{Settings: settings})
	FireOnEvaluationModelCallEnd(ctx, EvaluationModelCallEndEvent{DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: settings}})
	FireOnEvaluateEnd(ctx, EvaluateEndEvent{DecideStartEvent: DecideStartEvent{Settings: settings}})
	want := []string{"start", "mstart", "mend", "end"}
	if len(rec.calls) != 4 {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
	for i := range want {
		if rec.calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", rec.calls, want)
		}
	}
}
