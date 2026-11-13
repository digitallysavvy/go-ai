package bridge

import "testing"

// The bridge frame schemas are zod objects: keys match exactly, so a
// differently cased key is stripped instead of filling the field
// (encoding/json matched it case-insensitively, the last one winning).
func TestR97carryBridgeFramesMatchKeysExactly(t *testing.T) {
	msg, _, err := DecodeOutbound([]byte(`{"type":"sandbox-log","source":"s","stream":"stdout","line":"a","LINE":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.(*SandboxLog).Line; got != "a" {
		t.Fatalf("line = %q, want a", got)
	}
	cmd, err := DecodeInbound([]byte(`{"type":"resume","lastSeenEventId":3,"LastSeenEventID":9}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cmd.(*ResumeCommand).LastSeenEventID; got != 3 {
		t.Fatalf("lastSeenEventId = %v, want 3", got)
	}
}
