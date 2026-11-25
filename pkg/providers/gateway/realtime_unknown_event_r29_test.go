package gateway

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS serializeClientEvent is `return event`: an event type the SDK doesn't
// model is still forwarded.
func TestR29_GatewayRealtimeForwardsUnknownClientEvent(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://example.test/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	m := p.ExperimentalRealtime("openai/gpt-realtime")
	raw, err := m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "future-event", ItemID: "i1", EventID: "e1"})
	if err != nil || string(raw) != `{"eventId":"e1","itemId":"i1","type":"future-event"}` {
		t.Fatalf("%s %v", raw, err)
	}
}
