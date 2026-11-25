package agent

import (
	"context"
	"errors"
	"testing"

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

func (m *failingLoopModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.callCount < len(m.responses) {
		return m.mockLanguageModel.DoGenerate(ctx, opts)
	}
	return nil, m.err
}

// A run that fails or is aborted after a later step's PrepareCall override
// disabled telemetry still ends the loop's root with the settings it started
// with. (Step 0's PrepareCall runs before the root opens, so disabling
// telemetry there opens no root, as in TS.)
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
				Model: &failingLoopModel{err: tc.err, mockLanguageModel: mockLanguageModel{responses: []types.GenerateResult{{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
				}}}},
				Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "ok", nil
				}}},
				Telemetry: base,
				PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
					if c.StepNumber >= 1 {
						c.Telemetry = off
					}
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
