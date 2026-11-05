package workflow

import (
	"context"
	"regexp"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// r87ecarryCallIDs records the call ID of every root start event.
type r87ecarryCallIDs struct {
	telemetry.NoopTelemetryIntegration
	mu  sync.Mutex
	ids []string
}

func (c *r87ecarryCallIDs) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	c.ids = append(c.ids, e.CallID)
	c.mu.Unlock()
	return ctx
}

// TS workflow-agent-telemetry.ts gives a durable run's root, model steps and
// report the ID createIdGenerator({prefix: 'call', size: 24}) generates.
func TestR87ecarryWorkflowRootCallIDFormat(t *testing.T) {
	rec := &r87ecarryCallIDs{}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model:     &wfMockModel{},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^call-[0-9A-Za-z]{24}$`)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ids) != 2 {
		t.Fatalf("root starts = %d, want 2 (%v)", len(rec.ids), rec.ids)
	}
	for _, id := range rec.ids {
		if !re.MatchString(id) {
			t.Errorf("root call ID %q, want call- and 24 alphanumerics", id)
		}
	}
	if rec.ids[0] == rec.ids[1] {
		t.Errorf("two runs share the call ID %q", rec.ids[0])
	}
}
