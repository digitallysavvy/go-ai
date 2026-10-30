package workflow

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"

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
				// The root's own onEnd (with the call ID) records the
				// finish reason on each integration's root.
				hasFinish := false
				for _, kv := range s.Attributes() {
					if kv.Key == "ai.response.finishReason" {
						hasFinish = true
					}
				}
				if !hasFinish {
					t.Errorf("integration %s: root span has no ai.response.finishReason (its onEnd did not reach it)", name)
				}
			}
		}
		if started != 1 || ended != 1 {
			t.Errorf("integration %s: %s started %d, ended %d; want 1 and 1", name, workflowRootOperation, started, ended)
		}
	}
}

// TestR69IntegratorWorkflowStreamRootErrorPerIntegration: a failed run's
// root onError carries the call ID too, so each integration marks its own
// ai.workflowAgent.stream root as failed (without it, the error reached only
// the span in ctx, the last integration's).
func TestR69IntegratorWorkflowStreamRootErrorPerIntegration(t *testing.T) {
	recA, recB := tracetest.NewSpanRecorder(), tracetest.NewSpanRecorder()
	tpA := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recA))
	tpB := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recB))
	t.Cleanup(func() {
		_ = tpA.Shutdown(context.Background())
		_ = tpB.Shutdown(context.Background())
	})
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return nil, errors.New("open failed")
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
	_, _ = res.ReadAll()
	for name, rec := range map[string]*tracetest.SpanRecorder{"a": recA, "b": recB} {
		var statuses []codes.Code
		for _, s := range rec.Ended() {
			if s.Name() == workflowRootOperation {
				statuses = append(statuses, s.Status().Code)
			}
		}
		if len(statuses) != 1 || statuses[0] != codes.Error {
			t.Errorf("integration %s: %s ended with statuses %v, want one Error", name, workflowRootOperation, statuses)
		}
	}
}

// TestR69IntegratorWorkflowStreamRootAbortPerIntegration: a run cancelled
// mid-stream ends each integration's own root through onAbort with the call
// ID.
func TestR69IntegratorWorkflowStreamRootAbortPerIntegration(t *testing.T) {
	recA, recB := tracetest.NewSpanRecorder(), tracetest.NewSpanRecorder()
	tpA := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recA))
	tpB := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recB))
	t.Cleanup(func() {
		_ = tpA.Shutdown(context.Background())
		_ = tpB.Shutdown(context.Background())
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		cancel()
		return nil, context.Canceled
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
	res, err := w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = res.ReadAll()
	for name, rec := range map[string]*tracetest.SpanRecorder{"a": recA, "b": recB} {
		ended := 0
		for _, s := range rec.Ended() {
			if s.Name() == workflowRootOperation {
				ended++
			}
		}
		if ended != 1 {
			t.Errorf("integration %s: %s ended %d times, want 1", name, workflowRootOperation, ended)
		}
	}
}
