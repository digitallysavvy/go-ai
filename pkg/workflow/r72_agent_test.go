package workflow

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Round 72 (agent area) WorkflowAgent tests.

type r72agentToolTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts []map[string]interface{}
	ends   []map[string]interface{}
}

func (r *r72agentToolTelemetry) OnToolExecutionStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, e.ToolContext)
	return ctx
}

func (r *r72agentToolTelemetry) OnToolExecutionEnd(ctx context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e.ToolContext)
}

// TS createTelemetryDispatcher augments onToolExecutionStart/End with the
// tool context filtered by includeToolsContext (filterToolContext); the
// approval-resumed tool execution of WorkflowAgent dispatches the same events
// (executeToolWithCallbacks).
func TestR72agentWorkflowResumedToolTelemetryCarriesFilteredToolContext(t *testing.T) {
	rec := &r72agentToolTelemetry{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{},
		Tools: []types.Tool{{
			Name:         "getWeather",
			ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "sunny", nil
			},
		}},
		Telemetry: &ai.TelemetrySettings{
			IncludeToolsContext: map[string]map[string]bool{"getWeather": {"apiKeyID": true}},
			Integrations:        []telemetry.TelemetryIntegration{rec},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages:     workflowApprovalMessages(""),
		ToolsContext: map[string]interface{}{"getWeather": map[string]interface{}{"apiKeyID": "k1", "secret": "s"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Steps()
	want := map[string]interface{}{"apiKeyID": "k1"}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.starts) != 1 || !reflect.DeepEqual(rec.starts[0], want) {
		t.Fatalf("tool start contexts = %v", rec.starts)
	}
	if len(rec.ends) != 1 || !reflect.DeepEqual(rec.ends[0], want) {
		t.Fatalf("tool end contexts = %v", rec.ends)
	}
}
