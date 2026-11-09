package mcp

import (
	"encoding/json"
	"testing"
)

// TS MCPEventControlSchema (mcp-event-types.ts 45-55) and
// SubscribeEventResultSchema (27-34) use z.string() / z.boolean(), which
// reject null; encoding/json decoded null into "" / false, so Go accepted a
// gap with `cursor: null`, a termination with `message: null` and a
// subscribe result with `truncated: null`.
func TestR90toolsMCPEventNullFieldsRejected(t *testing.T) {
	h := newWebhookHarness(t)
	for name, value := range map[string]interface{}{
		"gap cursor":      map[string]interface{}{"type": "gap", "cursor": nil},
		"termination msg": map[string]interface{}{"type": "terminated", "error": map[string]interface{}{"code": 1, "message": nil}},
	} {
		if code := h.do(signedEventRequest(h.sub, value, "msg_1")); code != 400 {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
	if len(h.gaps) != 0 || len(h.terms) != 0 {
		t.Fatalf("callbacks ran: gaps=%d terms=%d", len(h.gaps), len(h.terms))
	}
	var r SubscribeEventResult
	if err := json.Unmarshal([]byte(`{"id":"sub_1","refreshBefore":null,"truncated":null}`), &r); err == nil {
		t.Error("subscribe result with truncated: null accepted")
	}
	if err := json.Unmarshal([]byte(`{"id":"sub_1","refreshBefore":null,"truncated":false}`), &r); err != nil {
		t.Errorf("valid subscribe result rejected: %v", err)
	}
}
