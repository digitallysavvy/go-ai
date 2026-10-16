package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS runs prepareCall before it resolves tool approvals: the hook sees the
// messages with the approval response still unresolved, and the runtime
// context it returns reaches the resumed tool execution. PrepareCall still
// runs exactly once.
func TestWorkflowApprovalResumeRunsAfterPrepareCall(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			var mu sync.Mutex
			var sawApprovalResponse, sawToolResult bool
			var toolRuntime interface{}
			model := &wfMockModel{}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: model,
				Tools: []types.Tool{{
					Name: "getWeather", ToolApproval: true,
					Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
						mu.Lock()
						toolRuntime = o.RuntimeContext
						mu.Unlock()
						return "sunny", nil
					},
				}},
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					mu.Lock()
					defer mu.Unlock()
					calls.Add(1)
					for _, m := range o.Messages {
						for _, p := range m.Content {
							switch p.(type) {
							case types.ToolApprovalResponseContent:
								sawApprovalResponse = true
							case types.ToolResultContent:
								sawToolResult = true
							}
						}
					}
					o.RuntimeContext = "from-hook"
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			msgs := workflowApprovalMessages("")
			if err := runWorkflow(t, w, stream, WorkflowGenerateOptions{Messages: msgs}, WorkflowStreamOptions{Messages: msgs}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls.Load() != 1 {
				t.Errorf("PrepareCall ran %d times, want 1", calls.Load())
			}
			if !sawApprovalResponse || sawToolResult {
				t.Errorf("hook messages: approval response seen=%v, tool result seen=%v; want unresolved", sawApprovalResponse, sawToolResult)
			}
			if toolRuntime != "from-hook" {
				t.Errorf("resumed tool saw runtime context %v, want the hook's", toolRuntime)
			}
			if got := lastToolResultInPrompt(t, model, "weather-1"); got.ToolCallID != "weather-1" {
				t.Errorf("model prompt lacks the resumed tool result: %+v", got)
			}
		})
	}
}
