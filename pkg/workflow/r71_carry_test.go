package workflow

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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

func (c *r71carryToolOrder) OnError(_ context.Context, _ telemetry.TelemetryErrorEvent) {
	c.add("telemetry:error")
}

// r71carryStreamOpenErrorModel fails to open its stream.
type r71carryStreamOpenErrorModel struct{ wfMockModel }

func (m *r71carryStreamOpenErrorModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, errors.New("boom")
}

// TS execute's catch awaits options.onError before telemetryDispatcher.onError.
func TestR71CarryWorkflowOnErrorBeforeTelemetryOnError(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := &r71carryToolOrder{}
			var model provider.LanguageModel = &wfErrorModel{}
			if mode == "stream" {
				model = &r71carryStreamOpenErrorModel{}
			}
			wa, err := NewWorkflowAgent(WorkflowAgent{
				Model:     model,
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
				OnError:   func(context.Context, error) { rec.add("callback:error") },
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "generate" {
				_, err = wa.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
			} else {
				var st *WorkflowStreamResult
				st, err = wa.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
				if err == nil {
					_, _ = st.ReadAll()
					err = st.Err()
				}
			}
			if err == nil {
				t.Fatal("expected error")
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			// In stream mode the delegated StreamText call reports its own
			// telemetry error first; the workflow root's comes after the
			// user callback.
			want := []string{"callback:error", "telemetry:error"}
			if n := len(rec.log); n < 2 || !reflect.DeepEqual(rec.log[n-2:], want) || (mode == "generate" && n != 2) {
				t.Fatalf("log = %v, want it to end with %v", rec.log, want)
			}
		})
	}
}

func (c *r71carryToolOrder) OnAbort(_ context.Context, _ telemetry.TelemetryAbortEvent) {
	c.add("telemetry:abort")
}

// r71carryCancelModel cancels the run's context from its model call.
type r71carryCancelModel struct {
	wfMockModel
	cancel context.CancelFunc
}

func (m *r71carryCancelModel) DoGenerate(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.cancel()
	return nil, ctx.Err()
}

// TS awaits options.onAbort before anything ends the run's telemetry; Go's
// root OnAbort comes after the callback.
func TestR71CarryWorkflowOnAbortBeforeTelemetryAbort(t *testing.T) {
	for _, mode := range []string{"generate-precancelled", "stream-precancelled", "generate-midrun"} {
		t.Run(mode, func(t *testing.T) {
			rec := &r71carryToolOrder{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var model provider.LanguageModel = &wfMockModel{}
			if mode == "generate-midrun" {
				model = &r71carryCancelModel{cancel: cancel}
			} else {
				cancel()
			}
			wa, err := NewWorkflowAgent(WorkflowAgent{
				Model:     model,
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
				OnAbort:   func(context.Context, []types.StepResult) { rec.add("callback:abort") },
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "stream-precancelled" {
				_, err = wa.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi"})
			} else {
				_, err = wa.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi"})
			}
			if err == nil {
				t.Fatal("expected error")
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			want := []string{"callback:abort", "telemetry:abort"}
			if n := len(rec.log); n < 2 || !reflect.DeepEqual(rec.log[n-2:], want) {
				t.Fatalf("log = %v, want it to end with %v", rec.log, want)
			}
		})
	}
}
