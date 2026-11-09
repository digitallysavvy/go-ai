package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type r90carryTextModel struct{ wfMockModel }

func (m *r90carryTextModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.opts = append(m.opts, opts)
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *r90carryTextModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.opts = append(m.opts, opts)
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

type r90carryStepTools struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	seen []string
}

func (r *r90carryStepTools) OnLanguageModelCallStart(ctx context.Context, e telemetry.LanguageModelCallStartEvent) context.Context {
	r.mu.Lock()
	r.seen = append(r.seen, fmt.Sprintf("%+v", e.Tools))
	r.mu.Unlock()
	return ctx
}

// TS workflow-agent-telemetry.integration.test.ts "redacts provider tools
// after the durable model-step boundary" (9f37b45262), with Go's
// openai.mcp rule: the GenAI tool definitions and the model-call tools a
// custom integration sees are redacted; the model still receives the credential.
func TestR90carryWorkflowRedactsProviderToolsInTelemetry(t *testing.T) {
	const secret = "workflow-test-secret"
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			steps := &r90carryStepTools{}
			model := &r90carryTextModel{}
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: model,
				Tools: []types.Tool{{
					Name: "remote", Type: types.ToolTypeProviderDefined, ProviderID: "openai.mcp", ProviderExecuted: true,
					ProviderArgs: map[string]interface{}{"serverLabel": "test", "authorization": secret},
					Parameters:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
				}},
				Telemetry: &telemetry.Options{
					IsEnabled: telemetry.Bool(true),
					Integrations: []telemetry.TelemetryIntegration{
						telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("wf")}), steps,
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "generate" {
				_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
			} else {
				var st *WorkflowStreamResult
				st, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hello"})
				if err == nil {
					_, err = st.ReadAll()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var defs []string
			for _, s := range rec.Ended() {
				for _, kv := range s.Attributes() {
					if kv.Key == attribute.Key("gen_ai.tool.definitions") {
						defs = append(defs, kv.Value.AsString())
					}
				}
			}
			if len(defs) != 1 || defs[0] != `[{"type":"provider","name":"remote","id":"openai.mcp","args":{"authorization":"[redacted]","serverLabel":"test"}}]` {
				t.Errorf("gen_ai.tool.definitions = %v", defs)
			}
			steps.mu.Lock()
			seen := strings.Join(steps.seen, "\n")
			steps.mu.Unlock()
			if seen == "" || strings.Contains(seen, secret) || !strings.Contains(seen, "[redacted]") {
				t.Errorf("step tools seen by the integration: %s", seen)
			}
			if len(model.opts) == 0 || model.opts[0].Tools[0].ProviderArgs["authorization"] != secret {
				t.Error("the model did not receive the real credential")
			}
		})
	}
}
