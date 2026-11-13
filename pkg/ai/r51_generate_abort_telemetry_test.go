package ai

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

type r51AbortRecorder struct {
	telemetry.NoopTelemetryIntegration
	errors, aborts int
}

func (r *r51AbortRecorder) OnError(context.Context, telemetry.TelemetryErrorEvent) { r.errors++ }
func (r *r51AbortRecorder) OnAbort(context.Context, telemetry.TelemetryAbortEvent) { r.aborts++ }

// TS generateText has no onAbort: a cancelled or timed-out call reports
// telemetry onError (and onStepError for the open step), so the legacy root
// and step spans carry the error status. The Go OnAbortEvent callback still
// fires.
func TestR51GenerateTextAbortReportsTelemetryError(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	counter := &r51AbortRecorder{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, context.Canceled
		},
	}
	aborted := false
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "hi",
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}), counter,
		}},
		OnAbortEvent: func(context.Context, GenerateTextAbortEvent) { aborted = true },
	})
	if !errors.Is(err, context.Canceled) || !aborted {
		t.Fatalf("err = %v, OnAbortEvent fired = %v", err, aborted)
	}
	if counter.errors != 1 || counter.aborts != 0 {
		t.Fatalf("OnError = %d, OnAbort = %d; want 1 and 0", counter.errors, counter.aborts)
	}
	for _, name := range []string{"ai.generateText", "ai.generateText.doGenerate"} {
		span := findSpanByName(rec.Ended(), name)
		if span == nil || span.Status().Code != codes.Error {
			t.Errorf("%s status = %+v, want error", name, span)
		}
	}
}
