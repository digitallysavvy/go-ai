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

// r71carryToolOrder records tool execution events of the telemetry
// integration and the user callbacks in one log.
type r71carryToolOrder struct {
	telemetry.NoopTelemetryIntegration
	mu  sync.Mutex
	log []string
}

func (c *r71carryToolOrder) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.log = append(c.log, s)
}

func (c *r71carryToolOrder) OnToolExecutionStart(ctx context.Context, _ telemetry.TelemetryToolCallStartEvent) context.Context {
	c.add("telemetry:start")
	return ctx
}

func (c *r71carryToolOrder) OnToolExecutionEnd(_ context.Context, _ telemetry.TelemetryToolCallFinishEvent) {
	c.add("telemetry:end")
}

// TS executeToolWithCallbacks (approval resume) awaits the merged
// onToolExecutionStart/End callback before telemetryDispatcher's.
func TestR71CarryWorkflowResumeToolCallbacksBeforeTelemetry(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := &r71carryToolOrder{}
			wa, err := NewWorkflowAgent(WorkflowAgent{
				Model: &wfMockModel{},
				Tools: []types.Tool{{
					Name:         "getWeather",
					ToolApproval: true,
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						rec.add("execute")
						return "sunny", nil
					},
				}},
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
				OnToolExecutionStart: func(context.Context, ai.OnToolCallStartEvent) {
					rec.add("callback:start")
				},
				OnToolExecutionEnd: func(context.Context, ai.OnToolCallFinishEvent) {
					rec.add("callback:end")
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			messages := []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "weather?"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "weather-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
					types.ToolApprovalRequestContent{ApprovalID: "approval-weather-1", ToolCallID: "weather-1"},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolApprovalResponseContent{ApprovalID: "approval-weather-1", Approved: true},
				}},
			}
			if mode == "generate" {
				_, err = wa.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages})
			} else {
				var st *WorkflowStreamResult
				st, err = wa.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: messages})
				if err == nil {
					_, err = st.ReadAll()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			want := []string{"callback:start", "telemetry:start", "execute", "callback:end", "telemetry:end"}
			if len(rec.log) < len(want) || !reflect.DeepEqual(rec.log[:len(want)], want) {
				t.Fatalf("log = %v, want prefix %v", rec.log, want)
			}
		})
	}
}
