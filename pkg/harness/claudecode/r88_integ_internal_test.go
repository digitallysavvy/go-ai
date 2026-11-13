package claudecode

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// TS claude-code-bridge-protocol.ts outboundMessageSchema intersects the
// shared outbound schema with `{ startId: z.string().optional() }`: a frame
// whose startId is present but not a string fails, on stream parts and
// control frames alike (carry r88 open item).
func TestR88integClaudeCodeRejectsNonStringStartID(t *testing.T) {
	for _, frame := range []string{
		`{"type":"stream-start","startId":5}`,
		`{"type":"stream-start","startId":null}`,
		`{"type":"stream-start","startId":{"id":"x"}}`,
		`{"type":"bridge-hello","startId":true}`,
	} {
		if msg, _, err := decodeOutbound([]byte(frame)); err == nil {
			t.Errorf("%s: accepted as %#v", frame, msg)
		}
	}
	msg, _, err := decodeOutbound([]byte(`{"type":"stream-start","startId":"turn-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := msg.(bridge.StreamPartFrame); !ok || f.StartID != "turn-1" {
		t.Fatalf("frame = %#v", msg)
	}
	if _, _, err := decodeOutbound([]byte(`{"type":"stream-start"}`)); err != nil {
		t.Fatalf("absent startId: %v", err)
	}
	if _, _, err := decodeOutbound([]byte(`{"type":"bridge-hello"}`)); err != nil {
		t.Fatalf("hello: %v", err)
	}
	// The shared decoder (other adapters) still ignores the key, as their TS
	// schemas strip it.
	if _, _, err := bridge.DecodeOutbound([]byte(`{"type":"stream-start","startId":5}`)); err != nil {
		t.Fatalf("shared decoder: %v", err)
	}
}
