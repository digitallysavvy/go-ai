package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// TS transport.onmessage reports a server message that has a method and no id
// through onError ("Unsupported message type").
func TestServerNotificationIsReportedAsUnsupportedMessageType(t *testing.T) {
	var mu sync.Mutex
	var got []string
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{OnError: func(err error) {
		mu.Lock()
		got = append(got, err.Error())
		mu.Unlock()
	}})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close() //nolint:errcheck

	transport.messages <- &MCPMessage{JSONRpc: "2.0", Method: "notifications/tools/list_changed"}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !strings.Contains(got[0], "Unsupported message type") {
		t.Fatalf("OnError calls = %v", got)
	}
}

// TS rejects resultType input_required in every protocol era.
func TestInputRequiredRejectedInLegacyEra(t *testing.T) {
	c := NewMCPClient(newMockTransport(), MCPClientConfig{})
	err := c.validateModernResult("tools/call", json.RawMessage(`{"resultType":"input_required"}`))
	if err == nil || !strings.Contains(err.Error(), "multi round-trip") {
		t.Fatalf("err = %v", err)
	}
	if err := c.validateModernResult("tools/call", json.RawMessage(`{"content":[]}`)); err != nil {
		t.Fatalf("legacy result without resultType: %v", err)
	}
}
