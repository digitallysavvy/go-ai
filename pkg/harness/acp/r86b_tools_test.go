package acp

import "testing"

// V061 item 26: a `name` decoded from the bridge frame is present even when
// it is "" (TS `toolCall.name != null`); null or absent is no name. The
// legacy resolver (TS resolve-host-tool-call.ts at 03dac28304^) only falls
// back to the `<server>-<tool>` title when `toolCall.name == null`.
func TestR86btoolsToolCallNamePresence(t *testing.T) {
	for _, c := range []struct {
		name     string
		frame    string
		hasName  bool
		resolves bool
	}{
		{"absent", `{"type":"acp-host-tool-call-resolution-request","requestId":"r","serverName":"host","toolNames":["search"],"toolCall":{"toolCallId":"c","title":"host-search","rawInput":{"q":1}}}`, false, true},
		{"null", `{"type":"acp-host-tool-call-resolution-request","requestId":"r","serverName":"host","toolNames":["search"],"toolCall":{"toolCallId":"c","title":"host-search","name":null,"rawInput":{"q":1}}}`, false, true},
		{"empty", `{"type":"acp-host-tool-call-resolution-request","requestId":"r","serverName":"host","toolNames":["search"],"toolCall":{"toolCallId":"c","title":"host-search","name":"","rawInput":{"q":1}}}`, true, false},
	} {
		msg, _, err := decodeOutbound([]byte(c.frame))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		f, ok := msg.(HostToolCallResolutionRequestFrame)
		if !ok {
			t.Fatalf("%s: decoded %T", c.name, msg)
		}
		if f.ToolCall.HasName() != c.hasName {
			t.Errorf("%s: HasName = %v, want %v", c.name, f.ToolCall.HasName(), c.hasName)
		}
		got := LegacyResolveHostToolCall(ResolveHostToolCallOptions{ToolCall: f.ToolCall, ServerName: f.ServerName, ToolNames: f.ToolNames})
		if (got != nil) != c.resolves {
			t.Errorf("%s: legacy resolved %+v, want resolves = %v", c.name, got, c.resolves)
		}
	}
	if !(ToolCall{Name: "x"}).HasName() || (ToolCall{}).HasName() {
		t.Error("a ToolCall built in Go has a name exactly when Name is not empty")
	}
}

// R86btoolsDecodeOutbound exposes the bridge frame decoder to the external
// test package (r86b_tools_ext_test.go).
var R86btoolsDecodeOutbound = decodeOutbound
