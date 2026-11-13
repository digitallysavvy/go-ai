package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type endRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	ends []telemetry.TelemetryFinishEvent
}

func (r *endRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e)
}

// Ports TS #22259: when a stop condition ends the turn, the end telemetry
// reports the usage of every completed step, not just the last one, and
// excludes the step after the stop condition.
func TestRunPrompt_StopConditionTelemetryReportsTotalUsage(t *testing.T) {
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(10, 1)},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(25, 4)},
				// Evaluated when the next part arrives; this step must not count.
				&TextDeltaPart{ID: "text-1", Delta: "ignored"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(7, 2)},
			}
		},
	})
	rec := &endRecorder{}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		StopConditions: []ai.StopCondition{func(s ai.StopConditionState) string {
			if len(s.Steps) == 2 {
				return "two-steps"
			}
			return ""
		}},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	drainRunPrompt(t, out)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ends) != 1 {
		t.Fatalf("end events = %d, want 1", len(rec.ends))
	}
	in := rec.ends[0].Usage.InputTokens
	if in == nil || *in != 35 {
		t.Fatalf("end usage input tokens = %v, want 35", in != nil)
	}
}
