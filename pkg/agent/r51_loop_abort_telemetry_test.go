package agent

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy loop runs like TS generateText, which has no abort telemetry: a
// cancelled run ends its root and step spans through onError, with the error
// status.
func TestR51LegacyLoopAbortRecordsErrorStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, context.Canceled
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Telemetry: &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
	}}})
	if _, err := a.Execute(context.Background(), "go"); err == nil {
		t.Fatal("Execute succeeded")
	}
	statuses := map[string]codes.Code{}
	for _, s := range rec.Ended() {
		statuses[s.Name()] = s.Status().Code
	}
	for _, name := range []string{"ai.generateText", "ai.generateText.doGenerate"} {
		if statuses[name] != codes.Error {
			t.Errorf("%s status = %v, want error (spans: %v)", name, statuses[name], statuses)
		}
	}
}
