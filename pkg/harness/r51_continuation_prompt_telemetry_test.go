package harness

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// A continuation turn carries no fresh prompt: TS passes `messages: []`, so
// the legacy root records ai.prompt {"messages":[]} and the GenAI root
// gen_ai.input.messages "[]" rather than leaving them out.
func TestR51ContinuationTurnRecordsEmptyMessages(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue", SandboxSession: testSandbox(), Model: "m",
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")}),
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
		}},
	})
	drainRunPrompt(t, out)
	var legacy, genai string
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if s.Name() == "ai.harness" && kv.Key == "ai.prompt" {
				legacy = kv.Value.AsString()
			}
			if s.Name() == "ai.harness m" && kv.Key == "gen_ai.input.messages" {
				genai = kv.Value.AsString()
			}
		}
	}
	if legacy != `{"messages":[]}` {
		t.Errorf("legacy ai.prompt = %q", legacy)
	}
	if genai != `[]` {
		t.Errorf("GenAI gen_ai.input.messages = %q", genai)
	}
}
