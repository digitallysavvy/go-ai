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
