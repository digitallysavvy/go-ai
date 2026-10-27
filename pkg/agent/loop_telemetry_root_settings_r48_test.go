package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// The loop's own root span is ended with the settings it was started with,
// so a PrepareCall override that disables telemetry cannot leave it open; and
// a root that was not opened because telemetry was off is opened when an
// override turns telemetry on, so the step spans have their root.
func TestLoopRootFollowsPrepareCallTelemetryOverride(t *testing.T) {
	for _, tc := range []struct {
		name             string
		baseOn, prepOn   bool
		wantRoot, wantSt bool
	}{
		{"override disables", true, false, true, false},
		{"override enables", false, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
			base := &telemetry.Options{IsEnabled: telemetry.Bool(tc.baseOn), Integrations: []telemetry.TelemetryIntegration{integration}}
			override := &telemetry.Options{IsEnabled: telemetry.Bool(tc.prepOn), Integrations: []telemetry.TelemetryIntegration{integration}}
			a := NewToolLoopAgent(AgentConfig{
				Model:     &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop}}},
				Telemetry: base,
				PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
					c.Telemetry = override
					return c
				},
			})
			if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
				t.Fatal(err)
			}
			if started, ended := len(rec.Started()), len(rec.Ended()); started != ended {
				t.Fatalf("%d spans started, %d ended", started, ended)
			}
			var root, step bool
			for _, s := range rec.Ended() {
				switch s.Name() {
				case "ai.generateText":
					root = true
				case "ai.generateText.doGenerate":
					step = true
				}
			}
			if root != tc.wantRoot || step != tc.wantSt {
				t.Fatalf("root=%v step=%v, want root=%v step=%v", root, step, tc.wantRoot, tc.wantSt)
			}
		})
	}
}
