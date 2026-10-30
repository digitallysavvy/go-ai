package workflow

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS gives every WorkflowAgent stream event the run's callId, and each
// integration keeps its own root span in callId-keyed state. With two
// integrations, each ends its own ai.workflowAgent.stream root once; without
// a call ID both fell back to the span in ctx (the last integration's), so
// the first integration's root leaked.
func TestR69WorkflowStreamRootPerIntegrationWithCallID(t *testing.T) {
	recA, recB := tracetest.NewSpanRecorder(), tracetest.NewSpanRecorder()
	tpA := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recA))
	tpB := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recB))
	t.Cleanup(func() {
		_ = tpA.Shutdown(context.Background())
		_ = tpB.Shutdown(context.Background())
	})
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tpA.Tracer("a")}),
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tpB.Tracer("b")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	for name, rec := range map[string]*tracetest.SpanRecorder{"a": recA, "b": recB} {
		started, ended := 0, 0
		for _, s := range rec.Started() {
			if s.Name() == workflowRootOperation {
				started++
			}
		}
		for _, s := range rec.Ended() {
			if s.Name() == workflowRootOperation {
				ended++
				if got := s.Attributes(); len(got) == 0 {
					t.Errorf("integration %s: root span has no attributes", name)
				}
			}
		}
		if started != 1 || ended != 1 {
			t.Errorf("integration %s: %s started %d, ended %d; want 1 and 1", name, workflowRootOperation, started, ended)
		}
	}
}
