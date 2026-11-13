package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type reasoningCaptureIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	steps []telemetry.TelemetryStepEndEvent
	ends  []telemetry.TelemetryFinishEvent
}

func (c *reasoningCaptureIntegration) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	c.mu.Lock()
	c.steps = append(c.steps, e)
	c.mu.Unlock()
}

func (c *reasoningCaptureIntegration) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.mu.Lock()
	c.ends = append(c.ends, e)
	c.mu.Unlock()
}

// TS legacy ai.response.reasoning joins the step's reasoning parts with "\n";
// the harness telemetry events carry that text and the parts.
func TestR49HarnessTelemetryReasoningJoinsParts(t *testing.T) {
	rec := &reasoningCaptureIntegration{}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ReasoningStartPart{ID: "r1"}, &ReasoningDeltaPart{ID: "r1", Delta: "a"}, &ReasoningEndPart{ID: "r1"},
				&ReasoningStartPart{ID: "r2"}, &ReasoningDeltaPart{ID: "r2", Delta: "b"}, &ReasoningEndPart{ID: "r2"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}})
	if err != nil {
		t.Fatal(err)
	}
	stream := res.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	_ = res.Err()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.steps) != 1 || rec.steps[0].Reasoning != "a\nb" || len(rec.steps[0].ReasoningParts) != 2 {
		t.Fatalf("step events = %+v, want reasoning \"a\\nb\" with two parts", rec.steps)
	}
	if len(rec.ends) != 1 || rec.ends[0].Reasoning != "a\nb" {
		t.Fatalf("end events = %+v, want reasoning \"a\\nb\"", rec.ends)
	}
}
