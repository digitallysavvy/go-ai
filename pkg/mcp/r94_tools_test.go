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

// TS requestWithInputRequired: an abort during a round is "Request was
// aborted" with the abort reason (options.signal.reason) as its cause.
func TestR94toolsInputRequiredAbortCauseIsReason(t *testing.T) {
	c, _ := r85toolsSetup(t,
		`{"resultType":"input_required","inputRequests":{"a":`+r85toolsElicitation+`},"requestState":"s"}`,
		r85toolsComplete,
	)
	reason := errors.New("stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	c.OnElicitationRequest(func(hctx context.Context, _ ElicitationRequest) (ElicitResult, error) {
		cancel(reason)
		<-hctx.Done()
		return ElicitResult{}, hctx.Err()
	})
	_, err := c.CallTool(ctx, "deploy", nil)
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "Request was aborted" || mcpErr.Cause != reason {
		t.Fatalf("err = %#v, want Request was aborted caused by the reason", err)
	}
}
