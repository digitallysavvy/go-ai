package mcp

import (
	"context"
	"encoding/json"
	"strings"
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

// TS createMCPEvents.subscribe (mcp-events.ts 104-131) parses the callback
// URL with WHATWG `new URL`, checks url.hash (empty for a bare "#") and
// stores url.href after searchParams.set, so the host is lowercased and a
// default port dropped. An unparseable URL is the TypeError "Invalid URL".
func TestR90toolsMCPEventCallbackURLIsWHATWGHref(t *testing.T) {
	store := newMemEventStore()
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: store})
	ev := client.ExperimentalEvents()
	ctx := context.Background()

	o := subOpts
	o.Delivery.URL = "HTTPS://App.Example:443/events?tenant=one"
	if _, err := ev.Subscribe(ctx, o); err != nil {
		t.Fatal(err)
	}
	delivery := tr.last("events/subscribe")["delivery"].(map[string]interface{})
	if u := delivery["url"].(string); !strings.HasPrefix(u, "https://app.example/events?tenant=one&"+MCPEventCallbackKey+"=") {
		t.Errorf("callback url = %q", u)
	}

	o.Delivery.URL = "https://app.example/events#"
	if _, err := ev.Subscribe(ctx, o); err != nil {
		t.Errorf("empty fragment rejected: %v", err)
	}

	o.Delivery.URL = "not a url"
	if _, err := ev.Subscribe(ctx, o); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid url error = %v", err)
	}
}
