package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDiagnosticChannelNilSafetyAndTypedMismatch(t *testing.T) {
	var nilChannel *DiagnosticChannel
	if err := nilChannel.Publish(context.Background(), DiagnosticMessage{Type: DiagnosticEventOnStart}); err != nil {
		t.Fatalf("nil channel Publish() error = %v", err)
	}

	unsub := nilChannel.Subscribe(func(context.Context, DiagnosticMessage) error { return nil })
	if unsub == nil {
		t.Fatal("expected non-nil unsubscribe func for nil channel")
	}
	unsub()

	typedUnsub := SubscribeDiagnosticTyped[TelemetryStartEvent](DiagnosticEventOnStart, nil)
	if typedUnsub == nil {
		t.Fatal("expected no-op unsubscribe for nil typed handler")
	}
	typedUnsub()

	typedUnsub = SubscribeDiagnosticTyped[TelemetryStartEvent](DiagnosticEventOnStart, func(context.Context, TelemetryStartEvent) error {
		return nil
	})
	defer typedUnsub()

	err := DefaultDiagnosticChannel().Publish(context.Background(), DiagnosticMessage{
		Type:  DiagnosticEventOnStart,
		Event: "wrong-payload-type",
	})
	if err == nil {
		t.Fatal("expected typed payload mismatch error")
	}
	if !strings.Contains(err.Error(), "diagnostic event onStart has payload string") {
		t.Fatalf("unexpected publish error: %v", err)
	}
}

func TestAddSettingsAttributesSetsSupportedTypes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("telemetry-test").Start(context.Background(), "span")
	AddSettingsAttributes(span, "ai.settings", map[string]interface{}{
		"str":   "value",
		"int":   3,
		"int64": int64(4),
		"float": 1.5,
		"bool":  true,
		"skip":  []string{"unsupported"},
	})
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}

	attrs := ended[0].Attributes()
	got := map[string]bool{}
	for _, attr := range attrs {
		got[string(attr.Key)] = true
	}

	for _, key := range []string{
		"ai.settings.str",
		"ai.settings.int",
		"ai.settings.int64",
		"ai.settings.float",
		"ai.settings.bool",
	} {
		if !got[key] {
			t.Fatalf("missing expected attribute key: %s", key)
		}
	}
	if got["ai.settings.skip"] {
		t.Fatal("unsupported type should not be emitted as attribute")
	}
}
