package telemetry

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/sdkversion"
)

// TS #23958: every diagnostic message identifies the emitting SDK version.
func TestDiagnosticChannelPublishAddsAIVersion(t *testing.T) {
	channel := NewDiagnosticChannel()
	var mu sync.Mutex
	var got []DiagnosticMessage
	channel.Subscribe(func(_ context.Context, msg DiagnosticMessage) error {
		mu.Lock()
		got = append(got, msg)
		mu.Unlock()
		return nil
	})

	event := struct{ CallID string }{"call-1"}
	if err := channel.Publish(context.Background(), DiagnosticMessage{Type: DiagnosticEventOnStart, Event: event}); err != nil {
		t.Fatal(err)
	}
	// An explicit version is kept.
	if err := channel.Publish(context.Background(), DiagnosticMessage{Type: DiagnosticEventOnEnd, Event: event, AIVersion: "other"}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("got %d messages", len(got))
	}
	if got[0].AIVersion != sdkversion.Version || got[0].Event != event {
		t.Fatalf("message 0 = %+v, want AIVersion %q and the original event", got[0], sdkversion.Version)
	}
	if got[1].AIVersion != "other" {
		t.Fatalf("message 1 AIVersion = %q, want the explicit value", got[1].AIVersion)
	}
}

func TestPublishDiagnosticAddsAIVersion(t *testing.T) {
	var mu sync.Mutex
	var got string
	unsubscribe := SubscribeDiagnostic(func(_ context.Context, msg DiagnosticMessage) error {
		mu.Lock()
		got = msg.AIVersion
		mu.Unlock()
		return nil
	})
	defer unsubscribe()
	PublishDiagnostic(context.Background(), DiagnosticEventOnStart, struct{}{})
	mu.Lock()
	defer mu.Unlock()
	if got != sdkversion.Version {
		t.Fatalf("AIVersion = %q, want %q", got, sdkversion.Version)
	}
}

// TS #23958: every event handed to an integration carries the SDK version,
// including the ones the caller pre-filled with something else.
type versionRecorder struct {
	NoopTelemetryIntegration
	mu      sync.Mutex
	start   string
	end     string
	toolVer string
	toolCtx string
}

func (r *versionRecorder) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	r.mu.Lock()
	r.start = e.AIVersion
	r.mu.Unlock()
	return ctx
}
func (r *versionRecorder) OnToolExecutionEnd(_ context.Context, e TelemetryToolCallFinishEvent) {
	r.mu.Lock()
	r.end = e.AIVersion
	r.mu.Unlock()
}
func (r *versionRecorder) ExecuteTool(ctx context.Context, _ string, args map[string]interface{}, next func(context.Context, map[string]interface{}) (interface{}, error)) (interface{}, error) {
	r.mu.Lock()
	r.toolCtx = AIVersionFromContext(ctx)
	r.mu.Unlock()
	return next(ctx, args)
}

func TestIntegrationEventsCarryAIVersion(t *testing.T) {
	rec := &versionRecorder{}
	settings := &Settings{IsEnabled: Bool(true), Integrations: []TelemetryIntegration{rec}}
	ctx := context.Background()
	FireOnStart(ctx, TelemetryStartEvent{Settings: settings, AIVersion: "spoofed"})
	FireOnToolCallFinish(ctx, TelemetryToolCallFinishEvent{Settings: settings})
	if _, err := FireExecuteToolWithSettings(ctx, settings, "t", nil, func(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.start != sdkversion.Version || rec.end != sdkversion.Version || rec.toolCtx != sdkversion.Version {
		t.Fatalf("versions = %q %q %q, want %q", rec.start, rec.end, rec.toolCtx, sdkversion.Version)
	}
}
