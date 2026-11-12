package mcp

import (
	"context"
	"errors"
	"testing"
)

// TS requestOnce: `response.result.resultType == null` is the modern
// missing-resultType error; a resultType that is present but not a string
// fails the result schema ("Failed to parse server response"), and a null
// result throws a TypeError inside the same try, so it is a parse failure
// too.
func TestR93toolsModernResultTypeNotString(t *testing.T) {
	for _, tc := range []struct{ result, want string }{
		{`{"resultType":5,"tools":[]}`, "Failed to parse server response"},
		{`null`, "Failed to parse server response"},
		{`{"resultType":null,"tools":[]}`, "Modern MCP result is missing resultType"},
		{`{"tools":[]}`, "Modern MCP result is missing resultType"},
	} {
		c, _ := r85toolsSetup(t, tc.result)
		_, err := c.ListTools(context.Background())
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || mcpErr.Message != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.result, err, tc.want)
		}
		_, err = c.CallTool(context.Background(), "deploy", nil)
		if !errors.As(err, &mcpErr) || mcpErr.Message != tc.want {
			t.Errorf("%s: call err = %v, want %q", tc.result, err, tc.want)
		}
	}
}

// TS events.subscribe: `!Number.isSafeInteger(ttlMs) || ttlMs <= 0` and the
// same for maxAgeMs, so a lifetime past 2^53 - 1 is rejected before any
// request.
func TestR93toolsEventsSubscribeSafeIntegerLifetime(t *testing.T) {
	tr := newEventsTransport()
	client := connectEventsClient(t, tr, &MCPEventsConfig{Store: newMemEventStore()})
	ev := client.ExperimentalEvents()
	big := int64(1 << 53)
	for _, opts := range []SubscribeEventOptions{
		{Name: subOpts.Name, Delivery: subOpts.Delivery, TTLMs: NullableOf(big)},
		{Name: subOpts.Name, Delivery: subOpts.Delivery, MaxAgeMs: &big},
	} {
		_, err := ev.Subscribe(context.Background(), opts)
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || mcpErr.Message != "Invalid MCP event subscription lifetime or replay age" {
			t.Fatalf("err = %v", err)
		}
	}
	ok := int64(1<<53 - 1)
	if _, err := ev.Subscribe(context.Background(), SubscribeEventOptions{Name: subOpts.Name, Delivery: subOpts.Delivery, TTLMs: NullableOf(ok)}); err != nil {
		t.Fatalf("max safe ttl: %v", err)
	}
}
