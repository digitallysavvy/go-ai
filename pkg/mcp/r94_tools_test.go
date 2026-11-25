package mcp

import (
	"context"
	"encoding/json"
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

// TS init(): an aborted initialization is "MCP client initialization was
// aborted" with externalSignal.reason as its cause.
func TestR94toolsConnectAbortCauseIsReason(t *testing.T) {
	reason := errors.New("stop")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	tr := &r85toolsMRTRTransport{messages: make(chan *MCPMessage, 64)}
	err := NewMCPClient(tr, MCPClientConfig{}).Connect(ctx)
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "MCP client initialization was aborted" || mcpErr.Cause != reason {
		t.Fatalf("err = %#v", err)
	}
}

// TS CallToolResultSchema's third arm (ResultSchema.extend({toolResult}))
// is loose: content that is not an array passes with a toolResult.
func TestR94toolsCallToolResultNonArrayContentWithToolResult(t *testing.T) {
	for _, tc := range []struct {
		result string
		ok     bool
	}{
		{`{"resultType":"complete","content":null,"toolResult":{"v":1}}`, true},
		{`{"resultType":"complete","content":"x","toolResult":1}`, true},
		{`{"resultType":"complete","content":null,"structuredContent":{}}`, false},
	} {
		c, _ := r85toolsSetup(t, tc.result)
		_, err := c.CallTool(context.Background(), "deploy", nil)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok %v", tc.result, err, tc.ok)
		}
	}
}

// The loose toolResult arm keeps content and isError as sent.
func TestR94toolsCallToolResultLooseArmRoundTrip(t *testing.T) {
	var r CallToolResult
	in := `{"content":"x","isError":"yes","toolResult":{"v":1}}`
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatal(err)
	}
	if err := validateCallToolResult(&r); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"content":"x","toolResult":{"v":1},"isError":"yes"}` && string(out) != `{"content":"x","isError":"yes","toolResult":{"v":1}}` {
		t.Fatalf("out = %s", out)
	}
}
