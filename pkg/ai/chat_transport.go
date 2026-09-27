package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ChatTransport sends a chat's message history to a remote or embedded
// backend and receives a stream of UI message chunks describing the
// response. It is the single Go counterpart of the TypeScript SDK's
// `ChatTransport<UIMessage>` interface (ai/packages/ai/src/ui/
// chat-transport.ts), which declares the same two methods
// (`sendMessages`/`reconnectToStream`) and is implemented directly by
// packages/workflow's WorkflowChatTransport and packages/langchain's
// LangSmithDeploymentTransport, and consumed by runAgentTUI's `transport`
// option (packages/tui/src/run-agent-tui.ts, agent-tui-runner.ts).
//
// This is the cross-package core concept referenced by pkg/tui's
// RunAgentTUIOptions.Transport. pkg/langchain.LangSmithDeploymentTransport
// implements this interface verbatim, matching its TS counterpart.
// pkg/workflow.WorkflowChatTransport does not yet: it is a separate
// server-side SSE run multiplexer (ServeHTTP/Resume) that predates the
// TS-parity port of packages/workflow's client-side WorkflowChatTransport
// (WORKFLOW-TRANSPORT), and closing that gap belongs to that PRD item, not
// here — do not add a second ChatTransport-shaped interface in pkg/workflow
// to paper over the difference in the meantime.
type ChatTransport interface {
	// SendMessages submits the current message history and returns a
	// channel of UI message chunks describing the response, plus an error
	// channel for transport-level failures. Both channels are closed when
	// the exchange completes. Implementations should stop sending on ctx
	// cancellation.
	SendMessages(ctx context.Context, req ChatTransportSendMessagesRequest) (<-chan UIMessageChunk, <-chan error)

	// ReconnectToStream resumes an in-progress streaming response for the
	// given chat session, mirroring TS ChatTransport.reconnectToStream.
	// Both returned channels are closed immediately, with no values or
	// errors, when there is no active stream to resume — the Go analog of
	// TS's `reconnectToStream` resolving to `null`.
	ReconnectToStream(ctx context.Context, req ChatTransportReconnectToStreamRequest) (<-chan UIMessageChunk, <-chan error)
}

// ChatTransportSendMessagesRequest is the request payload passed to
// ChatTransport.SendMessages. Field names mirror
// pkg/workflow.SendMessagesOptions (ChatID, MessageID, Trigger, Messages);
// Messages uses []types.Message — the same parameter type as
// pkg/langchain.LangSmithDeploymentTransport.SendMessages — since that is
// this SDK's canonical, provider-agnostic message history representation
// (the TypeScript SDK's ChatTransport instead carries []UIMessage, but the
// Go SDK has no cross-package UI-message-native history type used for
// conversation state; types.Message is the established convention).
type ChatTransportSendMessagesRequest struct {
	// ChatID identifies the conversation. Callers that run a single
	// conversation per process (such as pkg/tui's AgentTUIRunner) typically
	// generate this once per runner instance.
	ChatID string

	// MessageID is the id of the message being responded to, when known.
	MessageID string

	// Trigger describes why SendMessages was called (for example
	// "submit-message" or "regenerate-message"), matching the TypeScript
	// ChatTransport's `trigger` field.
	Trigger string

	// Messages is the full message history sent to the transport.
	Messages []types.Message
}

// ChatTransportReconnectToStreamRequest is the request payload passed to
// ChatTransport.ReconnectToStream, mirroring TS ChatTransport's
// `reconnectToStream` options (chatId; TS's `abortSignal` is expressed as
// the Go ctx parameter instead).
type ChatTransportReconnectToStreamRequest struct {
	// ChatID identifies the conversation whose in-progress stream should be
	// resumed.
	ChatID string
}
