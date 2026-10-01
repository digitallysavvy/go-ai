package mcp

import (
	"context"
	"testing"
)

// sendOptionsRecordingTransport is a minimal SendOptionsTransport used to
// verify MCPTransportSendOptions fields reach a transport unmodified,
// mirroring TS's MCPTransportSendOptions shape (mcp-transport.ts, hash
// 97f0565): relatedRequestId, resumptionToken, onresumptiontoken.
type sendOptionsRecordingTransport struct {
	lastMessage *MCPMessage
	lastOpts    MCPTransportSendOptions
}

func (t *sendOptionsRecordingTransport) Connect(context.Context) error { return nil }
func (t *sendOptionsRecordingTransport) Close() error                  { return nil }
func (t *sendOptionsRecordingTransport) Send(ctx context.Context, message *MCPMessage) error {
	return t.SendWithOptions(ctx, message, MCPTransportSendOptions{})
}
func (t *sendOptionsRecordingTransport) Receive(context.Context) (*MCPMessage, error) {
	return nil, context.Canceled
}
func (t *sendOptionsRecordingTransport) IsConnected() bool { return true }

func (t *sendOptionsRecordingTransport) SendWithOptions(_ context.Context, message *MCPMessage, opts MCPTransportSendOptions) error {
	t.lastMessage = message
	t.lastOpts = opts
	return nil
}

var (
	_ Transport            = (*sendOptionsRecordingTransport)(nil)
	_ SendOptionsTransport = (*sendOptionsRecordingTransport)(nil)
)

// TestSendOptionsTransportReceivesFullOptions confirms a transport
// implementing SendOptionsTransport receives relatedRequestId,
// resumptionToken, and onresumptiontoken unmodified, matching the fields
// TS's MCPTransportSendOptions declares (even though, as in TS, no built-in
// client code in this SDK currently populates them on its own send calls —
// see the doc comment on MCPTransportSendOptions).
func TestSendOptionsTransportReceivesFullOptions(t *testing.T) {
	transport := &sendOptionsRecordingTransport{}
	msg := &MCPMessage{JSONRpc: "2.0", ID: "req-1", Method: "notifications/progress"}

	var capturedToken string
	opts := MCPTransportSendOptions{
		RelatedRequestID: "req-1",
		ResumptionToken:  "resume-abc",
		OnResumptionToken: func(token string) {
			capturedToken = token
		},
	}

	if err := transport.SendWithOptions(context.Background(), msg, opts); err != nil {
		t.Fatalf("SendWithOptions error: %v", err)
	}
	if transport.lastMessage != msg {
		t.Fatalf("lastMessage = %v, want the same message pointer", transport.lastMessage)
	}
	if transport.lastOpts.RelatedRequestID != "req-1" {
		t.Fatalf("RelatedRequestID = %v, want req-1", transport.lastOpts.RelatedRequestID)
	}
	if transport.lastOpts.ResumptionToken != "resume-abc" {
		t.Fatalf("ResumptionToken = %q, want resume-abc", transport.lastOpts.ResumptionToken)
	}
	transport.lastOpts.OnResumptionToken("new-token")
	if capturedToken != "new-token" {
		t.Fatalf("OnResumptionToken did not reach the caller's callback: capturedToken = %q", capturedToken)
	}
}
