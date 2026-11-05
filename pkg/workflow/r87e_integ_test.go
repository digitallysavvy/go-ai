package workflow

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type r87eintegRootKey struct{}

// r87eintegOrder records the root start and returns a context value, so a
// callback can tell whether it runs inside the root.
type r87eintegOrder struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	events []string
}

func (o *r87eintegOrder) add(s string) {
	o.mu.Lock()
	o.events = append(o.events, s)
	o.mu.Unlock()
}

func (o *r87eintegOrder) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	o.add("telemetry-onStart")
	return context.WithValue(ctx, r87eintegRootKey{}, true)
}

// cr-r87e-gen-to-carry 1: TS WorkflowAgent awaits mergedOnStart, then
// telemetryDispatcher.onStart (workflow-agent.ts 2359-2390), so the user
// OnStart runs first and outside the root; later callbacks run inside it.
func TestR87eintegWorkflowUserOnStartBeforeRoot(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := &r87eintegOrder{}
			var inRoot, stepInRoot bool
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model:     &wfMockModel{},
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
				OnStart: func(ctx context.Context, _ ai.OnStartEvent) {
					rec.add("user-onStart")
					inRoot = ctx.Value(r87eintegRootKey{}) != nil
				},
				OnStepStart: func(ctx context.Context, _ ai.OnStepStartEvent) {
					stepInRoot = ctx.Value(r87eintegRootKey{}) != nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "generate" {
				if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
					t.Fatal(err)
				}
			} else {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := res.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if want := []string{"user-onStart", "telemetry-onStart"}; !reflect.DeepEqual(rec.events, want) {
				t.Fatalf("events = %v, want %v", rec.events, want)
			}
			if inRoot {
				t.Error("OnStart ran inside the telemetry root")
			}
			if !stepInRoot {
				t.Error("OnStepStart ran outside the telemetry root")
			}
		})
	}
}
