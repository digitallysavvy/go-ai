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

// TestWorkflowApprovalResumeToolSpanParentedUnderRootSpan mirrors the fix
// tracked by 27d294d ("Group orphaned tool calls after approvals under the
// parent span"): WorkflowAgent starts its own 'ai.workflowAgent.stream'
// operation span before pre-step approved-tool execution runs, so the
// approval-resume tool call's OTel span is a child of that root span instead
// of being silently skipped for lack of any parent in context.
func TestWorkflowApprovalResumeToolSpanParentedUnderRootSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("workflow-test")

	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
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
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}

	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "What's the weather?"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "weather-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
				types.ToolApprovalRequestContent{ApprovalID: "approval-weather-1", ToolCallID: "weather-1"},
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolApprovalResponseContent{ApprovalID: "approval-weather-1", Approved: true},
			}},
		},
	})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("stream ReadAll error: %v", err)
	}

	// The tool call span's real OTel name is the bare "ai.toolCall" (TS never
	// suffixes it with the tool name — see legacy-open-telemetry.ts
	// onToolExecutionStart / follow-up H1), so it is identified by its
	// ai.toolCall.name attribute instead.
	isGetWeatherToolSpan := func(s sdktrace.ReadOnlySpan) bool {
		if s.Name() != "ai.toolCall" {
			return false
		}
		for _, a := range s.Attributes() {
			if string(a.Key) == "ai.toolCall.name" && a.Value.AsString() == "getWeather" {
				return true
			}
		}
		return false
	}

	var rootSpan, toolSpan sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		switch {
		case s.Name() == "ai.workflowAgent.stream":
			rootSpan = s
		case isGetWeatherToolSpan(s):
			toolSpan = s
		}
	}
	for _, s := range rec.Started() {
		if s.Name() == "ai.workflowAgent.stream" && rootSpan == nil {
			rootSpan = s
		}
		if isGetWeatherToolSpan(s) && toolSpan == nil {
			toolSpan = s
		}
	}
	if rootSpan == nil {
		t.Fatal("expected an 'ai.workflowAgent.stream' root span")
	}
	if toolSpan == nil {
		t.Fatal("expected an 'ai.toolCall' span (ai.toolCall.name=getWeather) for the approval-resume tool execution")
	}
	if toolSpan.Parent().SpanID() != rootSpan.SpanContext().SpanID() {
		t.Fatalf("expected the approval-resume tool span's parent (%s) to be the root span (%s)",
			toolSpan.Parent().SpanID(), rootSpan.SpanContext().SpanID())
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
	want := map[string]int{"ai.workflowAgent.generate": 1, "ai.generateText.doGenerate": 2, "ai.toolCall": 1}
	for n, c := range want {
		if names[n] != c {
			t.Errorf("span %q count = %d, want %d (all: %v)", n, names[n], c, names)
		}
	}
}
