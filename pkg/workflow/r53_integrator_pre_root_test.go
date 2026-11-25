package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// r53PreRootCapture records root starts and errors.
type r53PreRootCapture struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts int
	errors int
}

func (c *r53PreRootCapture) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	return ctx
}

func (c *r53PreRootCapture) OnError(_ context.Context, _ telemetry.TelemetryErrorEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors++
}

// TS execute throws an invalid toolCallConcurrency, and a history whose tool
// calls lack results (convertToLanguageModelPrompt), before
// telemetryDispatcher.onStart, so neither the root start nor onError reaches
// the integrations. Go matches for generate and stream.
func TestR53IntegratorPreRootFailuresDispatchNoTelemetry(t *testing.T) {
	zero := 0
	unresolved := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "getWeather", Arguments: map[string]interface{}{}},
		}},
	}
	cases := []struct {
		name        string
		concurrency *int
		messages    []types.Message
	}{
		{"invalid toolCallConcurrency", &zero, []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		{"unresolved tool call", nil, unresolved},
	}
	for _, tc := range cases {
		for _, mode := range []string{"generate", "stream"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				capture := &r53PreRootCapture{}
				agent, err := NewWorkflowAgent(WorkflowAgent{
					Model: &wfMockModel{},
					Tools: []types.Tool{{
						Name: "getWeather",
						Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
							return "sunny", nil
						},
					}},
					Telemetry: &telemetry.Options{
						IsEnabled:    telemetry.Bool(true),
						Integrations: []telemetry.TelemetryIntegration{capture},
					},
				})
				if err != nil {
					t.Fatalf("NewWorkflowAgent: %v", err)
				}
				if mode == "generate" {
					_, err = agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: tc.messages, ToolCallConcurrency: tc.concurrency})
				} else {
					_, err = agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: tc.messages, ToolCallConcurrency: tc.concurrency})
				}
				if err == nil {
					t.Fatal("want an error before the root opens")
				}
				capture.mu.Lock()
				defer capture.mu.Unlock()
				if capture.starts != 0 || capture.errors != 0 {
					t.Fatalf("starts %d, errors %d; want no telemetry before the root", capture.starts, capture.errors)
				}
			})
		}
	}
}
