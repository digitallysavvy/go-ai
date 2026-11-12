package mcp

import (
	"context"
	"errors"
	"testing"
)

// TS send() starts with options.signal.throwIfAborted(), which throws the
// abort reason itself.
func TestR94toolsTransportSendAbortReason(t *testing.T) {
	reason := errors.New("stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	msg := &MCPMessage{JSONRpc: "2.0", Method: "ping"}
	if err := NewHTTPTransport(HTTPTransportConfig{URL: "http://127.0.0.1:1/mcp"}).Send(ctx, msg); err != reason {
		t.Fatalf("http send err = %v", err)
	}
	if err := NewSSETransport(SSETransportConfig{URL: "http://127.0.0.1:1/sse"}).Send(ctx, msg); err != reason {
		t.Fatalf("sse send err = %v", err)
	}
}
