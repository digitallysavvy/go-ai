package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Without a PrepareCall hook the WorkflowAgent root start event still
// carries the prompt as one user message (TS onStart messages).
func TestR52IntegratorWorkflowRootPromptMessageWithoutPrepareCall(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			capture := &r52StartCapture{}
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: &wfMockModel{},
				Telemetry: &telemetry.Options{
					IsEnabled:    telemetry.Bool(true),
					Integrations: []telemetry.TelemetryIntegration{capture},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "generate" {
				_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
			} else {
				var st *WorkflowStreamResult
				st, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
				if err == nil {
					_, err = st.ReadAll()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			capture.mu.Lock()
			defer capture.mu.Unlock()
			for _, e := range capture.starts {
				if !strings.HasPrefix(e.OperationType, "ai.workflowAgent.") {
					continue
				}
				if len(e.Messages) != 1 || e.Messages[0].Role != types.RoleUser || len(e.Messages[0].Content) != 1 {
					t.Fatalf("root messages = %+v, want the prompt as one user message", e.Messages)
				}
				if tc, ok := e.Messages[0].Content[0].(types.TextContent); !ok || tc.Text != "hi" {
					t.Fatalf("root message content = %+v", e.Messages[0].Content)
				}
				return
			}
			t.Fatalf("no workflow root start event in %+v", capture.starts)
		})
	}
}
