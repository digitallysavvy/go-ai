package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func r75carryRootAttrs(t *testing.T, rec *tracetest.SpanRecorder) map[string]string {
	t.Helper()
	for _, s := range rec.Ended() {
		if s.Parent().SpanID().IsValid() {
			continue
		}
		out := map[string]string{}
		for _, kv := range s.Attributes() {
			out[string(kv.Key)] = kv.Value.Emit()
		}
		return out
	}
	t.Fatal("no root span recorded")
	return nil
}

// TS onAudioOperationStart (open-telemetry.ts and legacy-open-telemetry.ts)
// records `text == null ? undefined : {input: () => text}`: generateSpeech's
// text is always present, so an empty string is still recorded (and, for the
// GenAI integration, folded into gen_ai.input.messages). Transcription events
// carry no text and record none.
func TestR75Carry_GenerateSpeechEmptyTextRecorded(t *testing.T) {
	type integ interface {
		OnStart(context.Context, TelemetryStartEvent) context.Context
		OnEnd(context.Context, TelemetryFinishEvent)
	}
	for _, tc := range []struct {
		name    string
		make    func(tp *sdktrace.TracerProvider) integ
		genAIIn bool
	}{
		{"genai", func(tp *sdktrace.TracerProvider) integ {
			return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}, true},
		{"legacy", func(tp *sdktrace.TracerProvider) integ {
			return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}, false},
	} {
		for _, op := range []string{"ai.generateSpeech", "ai.transcribe"} {
			t.Run(tc.name+"/"+op, func(t *testing.T) {
				rec := tracetest.NewSpanRecorder()
				tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
				t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
				in := tc.make(tp)
				settings := &Settings{IsEnabled: Bool(true)}
				ctx := in.OnStart(context.Background(), TelemetryStartEvent{
					CallID: "r75-speech", OperationType: op, ModelProvider: "mock-provider", ModelID: "mock-model-id", Settings: settings,
				})
				in.OnEnd(ctx, TelemetryFinishEvent{CallID: "r75-speech", OperationType: op, Settings: settings})
				attrs := r75carryRootAttrs(t, rec)
				text, hasText := attrs["ai.request.text"]
				msgs, hasMsgs := attrs["gen_ai.input.messages"]
				if op == "ai.generateSpeech" {
					if !hasText || text != "" {
						t.Errorf("ai.request.text = %q (present %v), want empty string recorded", text, hasText)
					}
					want := `[{"role":"user","parts":[{"type":"text","content":""}]}]`
					if tc.genAIIn && msgs != want {
						t.Errorf("gen_ai.input.messages = %q, want %s", msgs, want)
					}
				} else {
					if hasText {
						t.Errorf("transcribe recorded ai.request.text = %q", text)
					}
					if hasMsgs {
						t.Errorf("transcribe recorded gen_ai.input.messages = %q", msgs)
					}
				}
			})
		}
	}
}
