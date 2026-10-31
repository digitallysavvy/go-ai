package workflow

import (
	"context"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// r53OrderCapture records the order of root starts and tool events.
type r53OrderCapture struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	events []string
	starts []telemetry.TelemetryStartEvent
}

func (c *r53OrderCapture) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "start:"+e.OperationType)
	c.starts = append(c.starts, e)
	return ctx
}

func (c *r53OrderCapture) OnToolExecutionStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "tool:"+e.ToolName)
	return ctx
}

// TS WorkflowAgent runs the approved tools from the message history before
// it opens the root (telemetryDispatcher.onStart, operationId
// 'ai.workflowAgent.stream' for generate and stream alike), so the root's
// messages are the cleaned history and the resume's tool events find no
// call state: no tool span is recorded for them, not even under an ambient
// span in ctx.
func TestR53WorkflowResumeToolsRunBeforeRootOpens(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			tracer := tp.Tracer("workflow-test")
			capture := &r53OrderCapture{}
			agent, err := NewWorkflowAgent(WorkflowAgent{
				Model: &wfMockModel{},
				Tools: []types.Tool{
					{
						Name:         "getWeather",
						ToolApproval: true,
						Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
							return map[string]interface{}{"city": "London", "temperature": 72}, nil
						},
					},
				},
				Telemetry: &telemetry.Options{
					IsEnabled: telemetry.Bool(true),
					Integrations: []telemetry.TelemetryIntegration{
						capture,
						telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer}),
					},
				},
			})
			if err != nil {
				t.Fatalf("NewWorkflowAgent error: %v", err)
			}
			messages := []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "What's the weather?"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "weather-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
					types.ToolApprovalRequestContent{ApprovalID: "approval-weather-1", ToolCallID: "weather-1"},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolApprovalResponseContent{ApprovalID: "approval-weather-1", Approved: true},
				}},
			}
			ctx, ambient := tracer.Start(context.Background(), "ambient")
			if mode == "generate" {
				_, err = agent.GenerateWithOptions(ctx, WorkflowGenerateOptions{Messages: messages})
			} else {
				var st *WorkflowStreamResult
				st, err = agent.StreamWithOptions(ctx, WorkflowStreamOptions{Messages: messages})
				if err == nil {
					_, err = st.ReadAll()
				}
			}
			ambient.End()
			if err != nil {
				t.Fatal(err)
			}

			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.events) < 2 || capture.events[0] != "tool:getWeather" || capture.events[1] != "start:ai.workflowAgent.stream" {
				t.Fatalf("events = %v, want the resume tool before the 'ai.workflowAgent.stream' root", capture.events)
			}
			root := capture.starts[0]
			for _, m := range root.Messages {
				for _, part := range m.Content {
					switch part.(type) {
					case types.ToolApprovalRequestContent, types.ToolApprovalResponseContent:
						t.Fatalf("root messages keep approval part %#v; want the cleaned history", part)
					}
				}
			}
			var hasResult bool
			for _, m := range root.Messages {
				for _, part := range m.Content {
					if r, ok := part.(types.ToolResultContent); ok && r.ToolCallID == "weather-1" {
						hasResult = true
					}
				}
			}
			if !hasResult {
				t.Fatalf("root messages %+v lack the resumed tool result", root.Messages)
			}

			var sawRoot bool
			for _, s := range rec.Ended() {
				if s.Name() == "ai.workflowAgent.stream" {
					sawRoot = true
				}
				if s.Name() != "ai.toolCall" {
					continue
				}
				for _, a := range s.Attributes() {
					if string(a.Key) == "ai.toolCall.name" && a.Value.AsString() == "getWeather" {
						t.Fatalf("resume tool span recorded under %s; TS records none before the root", s.Parent().SpanID())
					}
				}
			}
			if !sawRoot {
				t.Fatal("expected an 'ai.workflowAgent.stream' root span")
			}
		})
	}
}

type runtimeContextCapture struct {
	telemetry.NoopTelemetryIntegration
	mu       sync.Mutex
	contexts []map[string]interface{}
}

func (c *runtimeContextCapture) record(rc map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contexts = append(c.contexts, rc)
}

func (c *runtimeContextCapture) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	c.record(e.RuntimeContext)
	return ctx
}

func (c *runtimeContextCapture) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	c.record(e.RuntimeContext)
	return ctx
}

func (c *runtimeContextCapture) OnFinish(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.record(e.RuntimeContext)
}

// TS #23340: model-step telemetry carries only the runtime context keys
// opted in through Telemetry.IncludeRuntimeContext.
func TestWorkflowTelemetryIncludesOnlyOptedInRuntimeContext(t *testing.T) {
	cases := []struct {
		name    string
		include map[string]bool
		want    map[string]interface{}
	}{
		{"unset", nil, map[string]interface{}{}},
		{"excluded", map[string]bool{"traceIdentity": false}, map[string]interface{}{}},
		{"included", map[string]bool{"traceIdentity": true}, map[string]interface{}{"traceIdentity": "u1"}},
	}
	for _, mode := range []string{"generate", "stream"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				capture := &runtimeContextCapture{}
				a, err := NewWorkflowAgent(WorkflowAgent{
					Model:          &wfMockModel{},
					RuntimeContext: map[string]interface{}{"traceIdentity": "u1", "secret": "s"},
					Telemetry: &telemetry.Options{
						IsEnabled:             telemetry.Bool(true),
						IncludeRuntimeContext: tc.include,
						Integrations:          []telemetry.TelemetryIntegration{capture},
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
				if len(capture.contexts) == 0 {
					t.Fatal("no telemetry events captured")
				}
				for i, rc := range capture.contexts {
					if len(rc) != len(tc.want) {
						t.Fatalf("event %d runtime context = %v, want %v", i, rc, tc.want)
					}
					for k, v := range tc.want {
						if rc[k] != v {
							t.Fatalf("event %d runtime context = %v, want %v", i, rc, tc.want)
						}
					}
				}
			})
		}
	}
}

// TS #22480: WorkflowAgent.generate dispatches step, model-call and tool-call
// telemetry to registered integrations, all under the workflow root span.
func TestWorkflowGenerateDispatchesStepAndModelCallTelemetry(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{},
		Tools: []types.Tool{{
			Name: "t1",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("wf")})},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	for _, s := range rec.Ended() {
		names[s.Name()]++
	}
	want := map[string]int{"ai.workflowAgent.stream": 1, "ai.generateText.doGenerate": 2, "ai.toolCall": 1}
	for n, c := range want {
		if names[n] != c {
			t.Errorf("span %q count = %d, want %d (all: %v)", n, names[n], c, names)
		}
	}
}
