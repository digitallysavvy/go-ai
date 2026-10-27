package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type failingLoopModel struct {
	mockLanguageModel
	err error
}

func (m *failingLoopModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, m.err
}

// A run that fails or is aborted after a PrepareCall override disabled
// telemetry still ends the loop's root with the settings it started with.
func TestLoopRootEndsOnErrorAndAbortAfterTelemetryOverride(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"error", errors.New("boom")},
		{"abort", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
			base := &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integration}}
			off := &telemetry.Options{IsEnabled: telemetry.Bool(false), Integrations: []telemetry.TelemetryIntegration{integration}}
			a := NewToolLoopAgent(AgentConfig{
				Model:     &failingLoopModel{err: tc.err},
				Telemetry: base,
				PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
					c.Telemetry = off
					return c
				},
			})
			if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err == nil {
				t.Fatal("GenerateAgent succeeded")
			}
			if started, ended := len(rec.Started()), len(rec.Ended()); started == 0 || started != ended {
				t.Fatalf("%d spans started, %d ended", started, ended)
			}
		})
	}
}

// A run whose context expired is an abort even when the error does not wrap
// the context error (GenerateText checks the context too).
func TestLegacyLoopExpiredContextIsAbort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if !isAbort(ctx, errors.New("provider gave up")) {
		t.Fatal("an error under an expired context is not an abort")
	}
}
