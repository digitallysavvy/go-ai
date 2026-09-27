package workflow

import (
	"context"
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
		Telemetry: &telemetry.Settings{
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

	var rootSpan, toolSpan sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		switch s.Name() {
		case "ai.workflowAgent.stream":
			rootSpan = s
		case "ai.toolCall.getWeather":
			toolSpan = s
		}
	}
	for _, s := range rec.Started() {
		if s.Name() == "ai.workflowAgent.stream" && rootSpan == nil {
			rootSpan = s
		}
		if s.Name() == "ai.toolCall.getWeather" && toolSpan == nil {
			toolSpan = s
		}
	}
	if rootSpan == nil {
		t.Fatal("expected an 'ai.workflowAgent.stream' root span")
	}
	if toolSpan == nil {
		t.Fatal("expected an 'ai.toolCall.getWeather' span for the approval-resume tool execution")
	}
	if toolSpan.Parent().SpanID() != rootSpan.SpanContext().SpanID() {
		t.Fatalf("expected the approval-resume tool span's parent (%s) to be the root span (%s)",
			toolSpan.Parent().SpanID(), rootSpan.SpanContext().SpanID())
	}
}
