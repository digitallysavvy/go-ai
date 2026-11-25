package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type r52StartCapture struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts []telemetry.TelemetryStartEvent
}

func (c *r52StartCapture) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	c.starts = append(c.starts, e)
	c.mu.Unlock()
	return ctx
}

type r52PreparedModel struct{ wfMockModel }

func (m *r52PreparedModel) ModelID() string { return "prepared-model" }

// TS WorkflowAgent's root start event (telemetryDispatcher.onStart) carries
// the call's messages, the generation settings after prepareCall, maxRetries
// (default 2) and the headers tagged ai-sdk-agent/workflow; prepareCall runs
// before the root opens, so its model and settings are the root's.
func TestR52WorkflowRootStartEventCarriesPreparedRequest(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			capture := &r52StartCapture{}
			temp, prepTemp, seed := 0.2, 0.7, 42
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model:       &wfMockModel{},
				Temperature: &temp,
				Seed:        &seed,
				Headers:     map[string]string{"x-agent": "1"},
				Telemetry: &telemetry.Options{
					IsEnabled:    telemetry.Bool(true),
					Integrations: []telemetry.TelemetryIntegration{capture},
				},
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.Model = &r52PreparedModel{}
					o.Temperature = &prepTemp
					return o, nil
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
			var root *telemetry.TelemetryStartEvent
			for i := range capture.starts {
				if strings.HasPrefix(capture.starts[i].OperationType, "ai.workflowAgent.") {
					root = &capture.starts[i]
					break
				}
			}
			if root == nil {
				t.Fatalf("no workflow root start event in %+v", capture.starts)
			}
			if root.ModelID != "prepared-model" {
				t.Errorf("root model = %q, want the prepareCall model", root.ModelID)
			}
			if len(root.Messages) != 1 {
				t.Errorf("root messages = %+v, want the prompt message", root.Messages)
			}
			if root.Temperature == nil || *root.Temperature != prepTemp || root.Seed == nil || *root.Seed != seed {
				t.Errorf("root settings temperature=%v seed=%v", root.Temperature, root.Seed)
			}
			if root.MaxRetries == nil || *root.MaxRetries != 2 {
				t.Errorf("root maxRetries = %v, want 2", root.MaxRetries)
			}
			if root.Headers["x-agent"] != "1" || !strings.Contains(root.Headers["user-agent"], "ai-sdk-agent/workflow") {
				t.Errorf("root headers = %v", root.Headers)
			}
		})
	}
}
